package relayclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/relayproto"
	"github.com/plutack/wiretap/internal/store"
	"github.com/plutack/wiretap/internal/testutil"
)

// fakeWebhookTransformer is an in-memory WebhookTransformer for the on_webhook
// wiring tests. transform, if set, rewrites the row; err, if set, is returned.
type fakeWebhookTransformer struct {
	transform func(store.WebhookRow) store.WebhookRow
	err       error
	calls     int
}

func (f *fakeWebhookTransformer) TransformWebhook(_ context.Context, row store.WebhookRow) (store.WebhookRow, error) {
	f.calls++
	if f.err != nil {
		return row, f.err
	}
	if f.transform != nil {
		row = f.transform(row)
	}
	return row, nil
}

func TestClient_transformWebhook(t *testing.T) {
	t.Parallel()
	base := store.WebhookRow{Project: "p", Seq: 1, Method: "POST", Path: "/x", Body: []byte("orig")}

	tests := []struct {
		name         string
		transformer  WebhookTransformer
		wantRejected bool
		wantBody     string
	}{
		{
			name:        "nil transformer stores as received",
			transformer: nil,
			wantBody:    "orig",
		},
		{
			name: "rewrite is applied",
			transformer: &fakeWebhookTransformer{transform: func(r store.WebhookRow) store.WebhookRow {
				r.Body = []byte("rewritten")
				return r
			}},
			wantBody: "rewritten",
		},
		{
			name:         "rejection drops the row",
			transformer:  &fakeWebhookTransformer{err: ErrWebhookRejected},
			wantRejected: true,
			wantBody:     "orig",
		},
		{
			name:        "non-reject error is non-fatal, row unchanged",
			transformer: &fakeWebhookTransformer{err: errors.New("boom")},
			wantBody:    "orig",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := &Client{transformer: tc.transformer}
			row, rejected := c.transformWebhook(context.Background(), base)
			if rejected != tc.wantRejected {
				t.Errorf("rejected = %v, want %v", rejected, tc.wantRejected)
			}
			if string(row.Body) != tc.wantBody {
				t.Errorf("row.Body = %q, want %q", row.Body, tc.wantBody)
			}
		})
	}
}

// TestClient_OnWebhookRejectAcksButDoesNotStore drives a full session: a PUSH
// arrives, the on_webhook transformer rejects it, and we assert the client
// still ACKs (so the relay advances its cursor) but never persists the row.
func TestClient_OnWebhookRejectAcksButDoesNotStore(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := freshPCStore(t)

	conn := NewFakeConn()
	dialer := &FakeDialer{}
	dialer.Queue(conn)

	c := New(Config{
		URL:         "ws://relay.example/tunnel",
		ClientID:    "c1",
		ClientToken: "t1",
		Projects:    []string{"project-a"},
	}, st,
		WithClock(&testutil.FakeClock{T: fixedClientTime}),
		WithDialer(dialer),
		WithBackoff(FixedBackoff{Duration: 0}),
		WithWebhookTransformer(&fakeWebhookTransformer{err: ErrWebhookRejected}),
	)

	runErr := make(chan error, 1)
	go func() { runErr <- c.Run(ctx) }()

	<-conn.ToServer // HELLO
	if err := conn.Send(relayproto.OK{
		Base:       relayproto.Base{Type: relayproto.TypeOK},
		Projects:   []string{"project-a"},
		ResumeFrom: map[string]int64{"project-a": 0},
	}); err != nil {
		t.Fatalf("send OK: %v", err)
	}

	if err := conn.Send(relayproto.Push{
		Base:    relayproto.Base{Type: relayproto.TypePush},
		Project: "project-a", Seq: 1, Method: "POST", Path: "/x",
		Body: []byte("dropme"), ReceivedAt: fixedClientTime.Unix(),
	}); err != nil {
		t.Fatalf("send PUSH: %v", err)
	}

	// The client must still ACK the rejected webhook.
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
		t.Fatal("timed out waiting for ACK on rejected webhook")
	}

	// The store must NOT have the row: LastSeq stays 0.
	// Poll briefly since the ACK send and the (skipped) store happen in order.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if got, _ := st.LastSeq(ctx, "project-a"); got != 0 {
			t.Fatalf("store LastSeq = %d, want 0 (rejected webhook must not persist)", got)
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	select {
	case <-runErr:
	case <-time.After(time.Second):
		t.Fatal("Run did not exit after ctx cancel")
	}
}
