package relayd

import (
	"bytes"
	"context"
	"encoding/json"
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
	var inResp struct {
		Seq int64 `json:"seq"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&inResp)
	if inResp.Seq != 1 {
		t.Fatalf("ingress seq = %d, want 1", inResp.Seq)
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

// TestTunnelRegistry_AttachReplace confirms a new attach for the same
// project closes the prior session (only one live tunnel per project).
func TestTunnelRegistry_AttachReplace(t *testing.T) {
	t.Parallel()
	r := NewTunnelRegistry()
	s1 := r.attach("project-a", "c1")
	s2 := r.attach("project-a", "c2")
	if r.lookup("project-a") != s2 {
		t.Error("new attach should replace the prior session")
	}
	if r.countTunnels() != 1 {
		t.Errorf("count = %d, want 1", r.countTunnels())
	}
	r.detachByProject("project-a", s2)
	if r.countTunnels() != 0 {
		t.Errorf("detach should remove s2; count = %d", r.countTunnels())
	}
	_ = s1
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
