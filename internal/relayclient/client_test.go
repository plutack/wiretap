package relayclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/relayproto"
	"github.com/plutack/wiretap/internal/store"
	"github.com/plutack/wiretap/internal/testutil"
)

// freshPCStore stands up a migrated in-memory PCStore for a test. Mirrors
// internal/store/pc_test.go's freshPCStore so the relayclient tests are
// isolated to this package.
func freshPCStore(t *testing.T) *store.PCStore {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenInMemory(fmt.Sprintf("relayclient-%s", t.Name()))
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.MigratePC(ctx, db); err != nil {
		t.Fatalf("MigratePC: %v", err)
	}
	return store.NewPCStore(db)
}

var fixedClientTime = time.Unix(1_700_000_000, 0).UTC()

// TestClient_HappyPath runs one full session end-to-end using in-memory
// doubles:
//   - FakeDialer returns one FakeConn
//   - test enqueues OK then a PUSH for project-a seq=1
//   - client stores the webhook, acks up_to_seq=1, fires OnWebhook
//   - test reads the ACK from ToServer and asserts on the cursor update
func TestClient_HappyPath(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := freshPCStore(t)

	conn := NewFakeConn()
	dialer := &FakeDialer{}
	dialer.Queue(conn)
	bb := FixedBackoff{Duration: 0} // instant retries

	gotWebhooks := make(chan store.WebhookRow, 8)
	gotConnect := make(chan []string, 1)

	c := New(Config{
		URL:         "ws://relay.example/tunnel",
		ClientID:    "c1",
		ClientToken: "t1",
		Projects:    []string{"project-a"},
	}, st,
		WithClock(&testutil.FakeClock{T: fixedClientTime}),
		WithDialer(dialer),
		WithBackoff(bb),
		WithCallbacks(Callbacks{
			OnWebhook: func(r store.WebhookRow) { gotWebhooks <- r },
			OnConnect: func(p []string) { gotConnect <- p },
		}),
	)

	runErr := make(chan error, 1)
	go func() { runErr <- c.Run(ctx) }()

	// Wait for the client to send HELLO. We can also assert on it.
	select {
	case helloBytes := <-conn.ToServer:
		m, err := relayproto.Decode(helloBytes)
		if err != nil {
			t.Fatalf("decode HELLO: %v", err)
		}
		h, ok := m.(relayproto.Hello)
		if !ok {
			t.Fatalf("expected Hello, got %T", m)
		}
		if h.ClientID != "c1" {
			t.Errorf("HELLO ClientID = %q, want c1", h.ClientID)
		}
		if got := h.LastSeqs["project-a"]; got != 0 {
			t.Errorf("HELLO last_seqs for project-a = %d, want 0 (empty store)", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for HELLO")
	}

	// Send OK.
	if err := conn.Send(relayproto.OK{
		Base:       relayproto.Base{Type: relayproto.TypeOK},
		Projects:   []string{"project-a"},
		ResumeFrom: map[string]int64{"project-a": 0},
	}); err != nil {
		t.Fatalf("send OK: %v", err)
	}

	select {
	case projects := <-gotConnect:
		if len(projects) != 1 || projects[0] != "project-a" {
			t.Errorf("OnConnect projects = %v, want [project-a]", projects)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for OnConnect")
	}

	// Send PUSH project-a seq=1.
	push := relayproto.Push{
		Base:    relayproto.Base{Type: relayproto.TypePush},
		Project: "project-a", Seq: 1, Method: "POST", Path: "/x",
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
		Body:       []byte(`{"hello":"world"}`),
		ReceivedAt: fixedClientTime.Unix(),
	}
	if err := conn.Send(push); err != nil {
		t.Fatalf("send PUSH: %v", err)
	}

	// Receive ACK.
	select {
	case ackBytes := <-conn.ToServer:
		m, err := relayproto.Decode(ackBytes)
		if err != nil {
			t.Fatalf("decode ACK: %v", err)
		}
		ack, ok := m.(relayproto.Ack)
		if !ok {
			t.Fatalf("expected Ack, got %T", m)
		}
		if ack.Project != "project-a" || ack.UpToSeq != 1 {
			t.Errorf("ACK = %+v, want project-a/up_to_seq=1", ack)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ACK")
	}

	// Confirm OnWebhook fired.
	select {
	case row := <-gotWebhooks:
		if row.Project != "project-a" || row.Seq != 1 {
			t.Errorf("OnWebhook row = %+v", row)
		}
		if !bytes.Equal(row.Body, []byte(`{"hello":"world"}`)) {
			t.Errorf("row Body = %q, want the raw body", row.Body)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for OnWebhook")
	}

	// Confirm the store persisted the webhook. LastSeq for project-a should
	// be 1.
	if got, _ := st.LastSeq(ctx, "project-a"); got != 1 {
		t.Errorf("store LastSeq = %d, want 1", got)
	}

	// Assert nothing died.
	select {
	case err := <-runErr:
		t.Fatalf("Run returned early: %v", err)
	default:
	}

	// Cancel ctx; expect ErrShutdown.
	cancel()
	select {
	case err := <-runErr:
		if !errors.Is(err, ErrShutdown) {
			t.Errorf("Run err = %v, want ErrShutdown", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not exit after ctx cancel")
	}
}

// TestClient_ReconnectAfterDisconnect confirms the Run loop reconnects and
// re-runs HELLO from the cursor the local store dictates — proving the
// ack-cursor resume semantics end-to-end on the PC side.
func TestClient_ReconnectAfterDisconnect(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := freshPCStore(t)

	conn1 := NewFakeConn()
	conn2 := NewFakeConn()
	dialer := &FakeDialer{}
	dialer.Queue(conn1)
	dialer.Queue(conn2)

	c := New(Config{
		URL:      "ws://relay.example/tunnel",
		ClientID: "c1", ClientToken: "t1",
		Projects: []string{"project-a"},
	}, st,
		WithClock(&testutil.FakeClock{T: fixedClientTime}),
		WithDialer(dialer),
		WithBackoff(FixedBackoff{Duration: 0}),
	)

	disconnects := make(chan error, 4)
	go func() {
		for err := range disconnects {
			_ = err
		}
	}()
	go func() { _ = c.Run(ctx) }()

	// First HELLO arrives on conn1.
	<-conn1.ToServer
	// Close conn1 abruptly — the reader returns errConnClosed.
	conn1.Close()

	// Wait a moment, then expect a HELLO on conn2. First pre-populate the
	// store so the next HELLO carries last_seqs != 0.
	if _, err := st.StoreWebhook(ctx, store.WebhookRow{
		Project: "project-a", Seq: 7, ReceivedAt: fixedClientTime,
		Method: "POST", HeadersJSON: "{}",
	}, fixedClientTime); err != nil {
		t.Fatalf("StoreWebhook seed: %v", err)
	}

	// HELLO on conn2 must carry last_seqs project-a=7.
	select {
	case b := <-conn2.ToServer:
		m, err := relayproto.Decode(b)
		if err != nil {
			t.Fatalf("decode HELLO: %v", err)
		}
		h, ok := m.(relayproto.Hello)
		if !ok {
			t.Fatalf("expected Hello, got %T", m)
		}
		if got := h.LastSeqs["project-a"]; got != 7 {
			t.Errorf("reconnect HELLO last_seqs = %d, want 7 (cursor persisted)", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect did not happen; no HELLO on conn2")
	}
}

// TestClient_DialFailureRetries confirms a dial error does not abort Run;
// it backs off and tries again. The first dial errors; the second succeeds.
func TestClient_DialFailureRetries(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := freshPCStore(t)

	conn := NewFakeConn()
	dialer := &FakeDialer{dialErr: errConnClosed}
	dialer.Queue(conn)
	c := New(Config{
		URL:      "ws://relay.example/tunnel",
		ClientID: "c1", ClientToken: "t1", Projects: []string{"project-a"},
	}, st,
		WithClock(&testutil.FakeClock{T: fixedClientTime}),
		WithDialer(dialer),
		WithBackoff(FixedBackoff{Duration: 0}),
	)
	go func() { _ = c.Run(ctx) }()

	// conn1 (which dialer returns on second call) should see a HELLO after
	// the first dial errored.
	select {
	case <-conn.ToServer:
		// HELLO arrived after retry — success.
	case <-time.After(2 * time.Second):
		t.Fatal("client did not retry dial after failure")
	}
	if dials := dialer.Dials(); dials < 2 {
		t.Errorf("Dials = %d, want at least 2", dials)
	}
	cancel()
}

// TestClient_RejectsNonOKFirstMessage confirms the session ends if the
// first message from the server is not OK (most likely an error response).
func TestClient_RejectsNonOKFirstMessage(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := freshPCStore(t)

	conn := NewFakeConn()
	dialer := &FakeDialer{}
	dialer.Queue(conn)

	var mu sync.Mutex
	var disconnectErr error
	c := New(Config{
		URL:      "ws://relay.example/tunnel",
		ClientID: "c1", ClientToken: "t1", Projects: []string{"project-a"},
	}, st,
		WithClock(&testutil.FakeClock{T: fixedClientTime}),
		WithDialer(dialer),
		WithBackoff(FixedBackoff{Duration: 100 * time.Hour}), // do not retry
		WithCallbacks(Callbacks{OnDisconnect: func(err error) {
			mu.Lock()
			disconnectErr = err
			mu.Unlock()
		}}),
	)
	go func() { _ = c.Run(ctx) }()

	<-conn.ToServer // HELLO
	// Send a Push instead of OK.
	_ = conn.Send(relayproto.Push{
		Base:    relayproto.Base{Type: relayproto.TypePush},
		Project: "project-a", Seq: 1, Method: "POST",
	})

	// OnDisconnect should fire with an "expected ok" error.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		err := disconnectErr
		mu.Unlock()
		if err != nil {
			if !contains(err.Error(), "expected ok") {
				t.Errorf("disconnect err = %v, want it to mention expected ok", err)
			}
			cancel()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("OnDisconnect did not fire after bad OK")
}

// TestExponentialBackoff_Monotonic confirms the backoff climbs up to Cap
// and never exceeds it.
func TestExponentialBackoff_Monotonic(t *testing.T) {
	t.Parallel()
	b := &ExponentialBackoff{Base: 10 * time.Millisecond, Cap: 100 * time.Millisecond, Jitter: 0}
	var prev time.Duration
	for i := 0; i < 20; i++ {
		got := b.Next()
		if got > 100*time.Millisecond {
			t.Errorf("Next() = %v, exceeds Cap 100ms", got)
		}
		if got < prev {
			t.Errorf("Next() = %v, dropped below previous %v (non-monotonic)", got, prev)
		}
		prev = got
	}
	if prev != 100*time.Millisecond {
		t.Errorf("after saturating, prev = %v, want 100ms (Cap, no jitter)", prev)
	}
}

// TestExponentialBackoff_Reset confirms Reset returns the base value.
func TestExponentialBackoff_Reset(t *testing.T) {
	t.Parallel()
	b := &ExponentialBackoff{Base: 10 * time.Millisecond, Cap: 100 * time.Millisecond, Jitter: 0}
	_ = b.Next()
	_ = b.Next()
	b.Reset()
	if got := b.Next(); got != 10*time.Millisecond {
		t.Errorf("after Reset, Next = %v, want base 10ms", got)
	}
}

// TestPushToRow_RoundTrip confirms pushToRow preserves body and headers; the
// inverse of relayd.rowToPush.
func TestPushToRow_RoundTrip(t *testing.T) {
	t.Parallel()
	p := relayproto.Push{
		Base:    relayproto.Base{Type: relayproto.TypePush},
		Project: "p", Seq: 9, Method: "POST", Path: "/orders/9",
		Headers:    map[string][]string{"X-Event": {"created"}, "Content-Type": {"application/json"}},
		RawHeaders: []byte("X-Event: created\r\nContent-Type: application/json\r\n"),
		Body:       []byte("raw-bytes"),
		ReceivedAt: 1_700_000_000,
		SourceIP:   "203.0.113.7",
	}
	row := pushToRow(p)
	if row.Project != "p" || row.Seq != 9 {
		t.Errorf("basic fields = %+v", row)
	}
	if !bytes.Equal(row.Body, p.Body) {
		t.Errorf("Body = %q, want %q", row.Body, p.Body)
	}
	if !bytes.Equal(row.RawHeaders, p.RawHeaders) {
		t.Errorf("RawHeaders mismatch")
	}
	if !row.ReceivedAt.Equal(fixedClientTime) {
		t.Errorf("ReceivedAt = %v, want %v", row.ReceivedAt, fixedClientTime)
	}
}

// TestBasicAuth confirms the basic auth header is well-formed.
func TestBasicAuth(t *testing.T) {
	t.Parallel()
	got := basicAuth("c1", "t1")
	if got == "" || got == "Basic " {
		t.Errorf("basicAuth returned empty value")
	}
	// Decoded value should be "c1:l1" style; we don't decode here but assert
	// the prefix.
	if !startsWith(got, "Basic ") {
		t.Errorf("basicAuth = %q, want it to start with 'Basic '", got)
	}
}

// contains is a tiny substring test helper used in a couple of assertions.
func contains(s, sub string) bool      { return bytes.Contains([]byte(s), []byte(sub)) }
func startsWith(s, prefix string) bool { return bytes.HasPrefix([]byte(s), []byte(prefix)) }
