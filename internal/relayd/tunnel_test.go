package relayd

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/plutack/wiretap/internal/relayproto"
	"github.com/plutack/wiretap/internal/store"
)

// dialTunnel opens a WebSocket to /tunnel as the wiretap PC would. Returns
// the conn ready for Read/Write. Auth is HTTP basic auth. The test fails
// if the dial itself does not succeed — use dialTunnelErr for tests that
// expect a failed handshake (e.g. bad auth).
func dialTunnel(t *testing.T, hs *httptest.Server, clientID, token string) *websocket.Conn {
	t.Helper()
	conn, err := dialTunnelErr(t, hs, clientID, token)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "test done") })
	return conn
}

// dialTunnelErr dials the tunnel and returns the dial error instead of
// failing the test when dial fails. For tests that assert a refused
// handshake (bad auth, unknown client).
func dialTunnelErr(t *testing.T, hs *httptest.Server, clientID, token string) (*websocket.Conn, error) {
	t.Helper()
	u, _ := url.Parse(hs.URL)
	u.Scheme = "ws"
	u.Path = "/tunnel"
	hdr := http.Header{}
	hdr.Set("Authorization", basicAuth(clientID, token))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, resp, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{
		HTTPHeader: hdr,
	})
	// coder/websocket returns resp non-nil on dial failure; examining its
	// body is optional. We close defensively but only when Body is present,
	// since some code paths hand back a constructed response without Body.
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func basicAuth(user, pass string) string {
	return "Basic " + base64(user+":"+pass)
}

// writeTunnel encodes and writes a single message frame; helpers below.
func writeTunnel(t *testing.T, ctx context.Context, conn *websocket.Conn, m relayproto.Message) {
	t.Helper()
	b, err := relayproto.Encode(m)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("Write: %v", err)
	}
}

// readTunnel blocks on a single message frame and returns the parsed Message.
func readTunnel(t *testing.T, ctx context.Context, conn *websocket.Conn) relayproto.Message {
	t.Helper()
	_, b, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	m, err := relayproto.Decode(b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return m
}

// TestTunnel_HappyPath is the relayd-side integration contract:
// register -> ingress (seq=1) -> attach tunnel -> HELLO{last_seqs=0} ->
// receive PUSH{seq=1} -> send ACK{up_to_seq=1} -> relay's acked_seq now 1.
//
// All against httptest.Server + in-memory SQLite + real WebSocket over the
// same TCP connection the production code uses.
func TestTunnel_HappyPath(t *testing.T) {
	t.Parallel()
	s, hs, _ := freshServer(t)
	makeClientFor(t, s, "c1", "secret-token", "project-a")

	// 1. Ingress POSTs a webhook before the tunnel is up.
	body := []byte(`{"hello":"world"}`)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, hs.URL+"/project-a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ingress Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ingress status = %d", resp.StatusCode)
	}

	// The webhook is stored but undelivered.
	if got, _ := s.store.PendingCount(context.Background(), "project-a"); got != 1 {
		t.Fatalf("pending = %d, want 1", got)
	}

	// 2. PC dials the tunnel.
	conn := dialTunnel(t, hs, "c1", "secret-token")
	ctx := context.Background()

	// 3. PC sends HELLO with last_seqs=0 (we have nothing locally yet).
	writeTunnel(t, ctx, conn, relayproto.Hello{
		Base:        relayproto.Base{Type: relayproto.TypeHello},
		ClientID:    "c1",
		ClientToken: "secret-token",
		LastSeqs:    map[string]int64{"project-a": 0},
	})

	// 4. Relay replies OK with the resume cursor.
	okMsg := readTunnel(t, ctx, conn)
	ok, isOk := okMsg.(relayproto.OK)
	if !isOk {
		t.Fatalf("expected OK message, got %T: %+v", okMsg, okMsg)
	}
	if len(ok.Projects) != 1 || ok.Projects[0] != "project-a" {
		t.Errorf("OK projects = %v, want [project-a]", ok.Projects)
	}

	// 5. Relay pushes the pending webhook (seq=1).
	pushMsg := readTunnel(t, ctx, conn)
	push, isPush := pushMsg.(relayproto.Push)
	if !isPush {
		t.Fatalf("expected PUSH, got %T: %+v", pushMsg, pushMsg)
	}
	if push.Project != "project-a" || push.Seq != 1 {
		t.Errorf("PUSH = %+v, want project-a/seq=1", push)
	}
	if string(push.Body) != `{"hello":"world"}` {
		t.Errorf("PUSH body = %q, want raw JSON", push.Body)
	}

	// 6. PC acks the webhook.
	writeTunnel(t, ctx, conn, relayproto.Ack{
		Base:    relayproto.Base{Type: relayproto.TypeAck},
		Project: "project-a",
		UpToSeq: 1,
	})

	// 7. The relay's acked_seq reflects the new cursor. We retry briefly
	// because the ACK is processed in the relay's read loop goroutine; it
	// may not have hit the store by the time we check.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if seq, _ := s.store.AckedSeq(context.Background(), "project-a"); seq == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if seq, _ := s.store.AckedSeq(context.Background(), "project-a"); seq != 1 {
		t.Fatalf("acked_seq = %d, want 1", seq)
	}
	if got, _ := s.store.PendingCount(context.Background(), "project-a"); got != 0 {
		t.Errorf("pending after ack = %d, want 0", got)
	}
}

// TestTunnel_RejectsBadToken confirms bad basic-auth produces a failed handshake.
func TestTunnel_RejectsBadToken(t *testing.T) {
	t.Parallel()
	s, hs, _ := freshServer(t)
	makeClientFor(t, s, "c1", "real-token", "project-a")

	conn, err := dialTunnelErr(t, hs, "c1", "wrong-token")
	if err == nil {
		_ = conn.Close(websocket.StatusNormalClosure, "")
		t.Fatal("Dial with wrong token: expected handshake failure, got success")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("Dial err = %v, want it to mention 401", err)
	}
}

// TestTunnel_HelloMismatchRejects confirms HELLO's client_id must match the
// basic-auth client_id.
func TestTunnel_HelloMismatchRejects(t *testing.T) {
	t.Parallel()
	s, hs, _ := freshServer(t)
	makeClientFor(t, s, "c1", "real-token", "project-a")

	conn := dialTunnel(t, hs, "c1", "real-token")
	defer conn.Close(websocket.StatusNormalClosure, "")
	writeTunnel(t, context.Background(), conn, relayproto.Hello{
		Base:        relayproto.Base{Type: relayproto.TypeHello},
		ClientID:    "different-id", // mismatch
		ClientToken: "real-token",
		LastSeqs:    map[string]int64{"project-a": 0},
	})
	_, _, err := conn.Read(context.Background())
	if err == nil {
		t.Error("expected Read to error on mismatched HELLO, got nil")
	}
}

// TestTunnel_LiveIngressPushed confirms ingress after a tunnel attaches is
// pushed straight down (the "store-and-forward for offline + immediate for
// online" pattern). The webhook should arrive without the PC polling.
func TestTunnel_LiveIngressPushed(t *testing.T) {
	t.Parallel()
	s, hs, _ := freshServer(t)
	makeClientFor(t, s, "c1", "secret-token", "project-a")

	conn := dialTunnel(t, hs, "c1", "secret-token")
	defer conn.Close(websocket.StatusNormalClosure, "")
	writeTunnel(t, context.Background(), conn, relayproto.Hello{
		Base: relayproto.Base{Type: relayproto.TypeHello}, ClientID: "c1", ClientToken: "secret-token",
		LastSeqs: map[string]int64{"project-a": 0},
	})
	_ = readTunnel(t, context.Background(), conn) // OK

	// Fire a webhook asynchronously. read may race the relay; the test holds.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, hs.URL+"/project-a", bytes.NewReader([]byte(`{"x":1}`)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	pushMsg := readTunnel(t, context.Background(), conn)
	wg.Wait()
	push, ok := pushMsg.(relayproto.Push)
	if !ok {
		t.Fatalf("expected PUSH, got %T", pushMsg)
	}
	if push.Seq != 1 {
		t.Errorf("PUSH seq = %d, want 1", push.Seq)
	}
}

func TestTunnel_FansOutWithIndependentAcknowledgements(t *testing.T) {
	t.Parallel()
	s, hs, _ := freshServer(t)
	makeClientFor(t, s, "c1", "token-1", "project-a")
	makeClientFor(t, s, "c2", "token-2")
	if err := s.store.SubscribeProject(context.Background(), "project-a", "c2", false, s.clock.Now()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connections := []struct {
		id    string
		token string
		conn  *websocket.Conn
	}{
		{id: "c1", token: "token-1"},
		{id: "c2", token: "token-2"},
	}
	for i := range connections {
		connections[i].conn = dialTunnel(t, hs, connections[i].id, connections[i].token)
		writeTunnel(t, ctx, connections[i].conn, relayproto.Hello{
			Base: relayproto.Base{Type: relayproto.TypeHello}, ClientID: connections[i].id,
			ClientToken: connections[i].token, LastSeqs: map[string]int64{"project-a": 0},
		})
		if _, ok := readTunnel(t, ctx, connections[i].conn).(relayproto.OK); !ok {
			t.Fatalf("%s did not receive OK", connections[i].id)
		}
	}

	postIngress(t, hs, "project-a", []byte(`{"shared":true}`), nil)
	for _, pc := range connections {
		push, ok := readTunnel(t, ctx, pc.conn).(relayproto.Push)
		if !ok || push.Seq != 1 {
			t.Fatalf("%s push = %#v", pc.id, push)
		}
	}
	writeTunnel(t, ctx, connections[0].conn, relayproto.Ack{
		Base: relayproto.Base{Type: relayproto.TypeAck}, Project: "project-a", UpToSeq: 1,
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if seq, _ := s.store.AckedSeqForClient(ctx, "project-a", "c1"); seq == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if seq, _ := s.store.AckedSeqForClient(ctx, "project-a", "c1"); seq != 1 {
		t.Fatalf("c1 ack = %d, want 1", seq)
	}
	if seq, _ := s.store.AckedSeqForClient(ctx, "project-a", "c2"); seq != 0 {
		t.Fatalf("c2 ack = %d, want 0", seq)
	}
	if pending, _ := s.store.PendingCount(ctx, "project-a"); pending != 1 {
		t.Fatalf("pending after one subscriber ack = %d, want 1", pending)
	}
}

// TestTunnel_Replay asks the relay to re-deliver an already-acked webhook.
func TestTunnel_Replay(t *testing.T) {
	t.Parallel()
	s, hs, _ := freshServer(t)
	makeClientFor(t, s, "c1", "secret-token", "project-a")

	// Ingress three webhooks before tunnel attaches.
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, hs.URL+"/project-a",
			bytes.NewReader([]byte(fmt.Sprintf(`{"i":%d}`, i))))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("ingress: %v", err)
		}
		_ = resp.Body.Close()
	}

	conn := dialTunnel(t, hs, "c1", "secret-token")
	defer conn.Close(websocket.StatusNormalClosure, "")
	writeTunnel(t, context.Background(), conn, relayproto.Hello{
		Base: relayproto.Base{Type: relayproto.TypeHello}, ClientID: "c1", ClientToken: "secret-token",
		LastSeqs: map[string]int64{"project-a": 0},
	})
	_ = readTunnel(t, context.Background(), conn) // OK
	// Receive all three pending PUSHes + ack them.
	for i := int64(1); i <= 3; i++ {
		p := readTunnel(t, context.Background(), conn)
		push, ok := p.(relayproto.Push)
		if !ok {
			t.Fatalf("expected PUSH %d, got %T", i, p)
		}
		if push.Seq != i {
			t.Fatalf("PUSH seq = %d, want %d", push.Seq, i)
		}
	}
	writeTunnel(t, context.Background(), conn, relayproto.Ack{
		Base: relayproto.Base{Type: relayproto.TypeAck}, Project: "project-a", UpToSeq: 3,
	})
	// Wait for ACK to settle.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if seq, _ := s.store.AckedSeq(context.Background(), "project-a"); seq == 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Ask the relay to replay seq=2.
	writeTunnel(t, context.Background(), conn, relayproto.Replay{
		Base:    relayproto.Base{Type: relayproto.TypeReplay},
		Project: "project-a", Seqs: []int64{2},
	})
	p := readTunnel(t, context.Background(), conn)
	push, ok := p.(relayproto.Push)
	if !ok {
		t.Fatalf("expected replay PUSH, got %T", p)
	}
	if push.Seq != 2 {
		t.Fatalf("replay seq = %d, want 2", push.Seq)
	}
}

func TestAdminReplay_TargetsOneSubscriber(t *testing.T) {
	t.Parallel()
	s, hs, admin := freshServer(t)
	makeClientFor(t, s, "c1", "token-1", "project-a")
	if err := s.store.CreateClient(context.Background(), "c2", "token-2", "two", s.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.store.SubscribeProject(context.Background(), "project-a", "c2", true, s.clock.Now()); err != nil {
		t.Fatal(err)
	}
	postIngress(t, hs, "project-a", []byte(`{"event":"saved"}`), nil)

	type connected struct {
		id    string
		token string
		conn  *websocket.Conn
	}
	clients := []connected{
		{id: "c1", token: "token-1", conn: dialTunnel(t, hs, "c1", "token-1")},
		{id: "c2", token: "token-2", conn: dialTunnel(t, hs, "c2", "token-2")},
	}
	for _, client := range clients {
		writeTunnel(t, context.Background(), client.conn, relayproto.Hello{
			Base: relayproto.Base{Type: relayproto.TypeHello}, ClientID: client.id,
			ClientToken: client.token, LastSeqs: map[string]int64{"project-a": 1},
		})
		if _, ok := readTunnel(t, context.Background(), client.conn).(relayproto.OK); !ok {
			t.Fatalf("%s did not receive OK", client.id)
		}
	}

	if err := admin.ReplayWebhookToClient(context.Background(), "project-a", 1, "c1"); err != nil {
		t.Fatalf("targeted replay: %v", err)
	}
	message := readTunnel(t, context.Background(), clients[0].conn)
	push, ok := message.(relayproto.Push)
	if !ok || push.Seq != 1 || push.Project != "project-a" {
		t.Fatalf("target replay = %#v", message)
	}

	readCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, _, err := clients[1].conn.Read(readCtx); err == nil {
		t.Fatal("non-target subscriber unexpectedly received replay")
	}
}

// TestTunnelRegistry_FanoutAndClientReconnect confirms a project retains one
// session per subscribed client while reconnecting replaces only that client.
func TestTunnelRegistry_FanoutAndClientReconnect(t *testing.T) {
	t.Parallel()
	r := NewTunnelRegistry()
	s1 := r.attach("c1", []string{"project-a"})
	s2 := r.attach("c2", []string{"project-a"})
	if len(r.sessions("project-a")) != 2 {
		t.Fatalf("project sessions = %d, want 2", len(r.sessions("project-a")))
	}
	if r.countTunnels() != 2 {
		t.Errorf("count = %d, want 2", r.countTunnels())
	}
	replacement := r.attach("c1", []string{"project-a", "project-b"})
	if r.countTunnels() != 2 || len(r.sessions("project-a")) != 2 {
		t.Errorf("client reconnect disturbed another subscriber")
	}
	select {
	case <-s1.done:
	default:
		t.Error("replaced c1 session remains open")
	}
	r.detach(replacement)
	r.detach(s2)
	if r.countTunnels() != 0 {
		t.Errorf("detach count = %d, want 0", r.countTunnels())
	}
}

// TestRowToPush confirms the WebhookRow -> Push conversion preserves body
// and the headers map. (SourceIP and ReceivedAt are exercised in tunnel
// tests above.)
func TestRowToPush(t *testing.T) {
	t.Parallel()
	row := store.WebhookRow{
		Project: "p", Seq: 42, ReceivedAt: time.Unix(1700000000, 0).UTC(),
		SourceIP: "10.0.0.1", Method: "POST", Path: "/x",
		HeadersJSON: `{"Content-Type":["application/json"]}`,
		RawHeaders:  []byte("Content-Type: application/json\r\n"),
		Body:        []byte("hello"),
	}
	p := rowToPush(row)
	if p.Project != "p" || p.Seq != 42 || p.Method != "POST" {
		t.Errorf("basic fields = %+v", p)
	}
	if p.ReceivedAt != 1700000000 {
		t.Errorf("ReceivedAt = %d, want 1700000000", p.ReceivedAt)
	}
	if !bytes.Equal(p.Body, []byte("hello")) {
		t.Errorf("Body = %q", p.Body)
	}
	if !bytes.Equal(p.RawHeaders, row.RawHeaders) {
		t.Errorf("RawHeaders mismatch")
	}
	if got, ok := p.Headers["Content-Type"]; !ok || len(got) != 1 || got[0] != "application/json" {
		t.Errorf("Headers = %+v", p.Headers)
	}
}

// base64 is a tiny stdlib wrapper so tunnel_test.go doesn't pull in
// encoding/base64 directly for one call. Standard padded base64.
func base64(s string) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	b := []byte(s)
	var out strings.Builder
	for i := 0; i < len(b); i += 3 {
		var n uint32
		var cnt int
		for j := 0; j < 3; j++ {
			if i+j < len(b) {
				n = (n << 8) | uint32(b[i+j])
				cnt++
			} else {
				n <<= 8
			}
		}
		out.WriteByte(alphabet[(n>>18)&0x3F])
		out.WriteByte(alphabet[(n>>12)&0x3F])
		if cnt > 1 {
			out.WriteByte(alphabet[(n>>6)&0x3F])
		} else {
			out.WriteByte('=')
		}
		if cnt > 2 {
			out.WriteByte(alphabet[n&0x3F])
		} else {
			out.WriteByte('=')
		}
	}
	return out.String()
}
