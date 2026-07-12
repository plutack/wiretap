package relayclient_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/relayclient"
	"github.com/plutack/wiretap/internal/relayd"
	"github.com/plutack/wiretap/internal/store"
	"github.com/plutack/wiretap/internal/testutil"
)

// This file runs the Phase 3 integration contract end-to-end:
//   relayd (httptest) + relayclient + two in-memory SQLite DBs
//   app POSTs webhook to relay -> PC stores it locally -> ACKs ->
//   relay's acked_seq advances. Across real WebSocket transport.
//
// We import internal/relayd (cross-package white-box), use its test helpers
// to spin up a Server, and dial it via the production WSNetDialer (no fake
// conn here).

// freshRelayServer stands up a relayd Server with a seeded client/project.
// Copied from internal/relayd/server_test.go; kept local to this _test file
// so cross-package tests don't have to depend on relayd's test helpers.
func freshRelayServer(t *testing.T, clientID, token string, projects ...string) (*relayd.Server, *httptest.Server) {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenInMemory(fmt.Sprintf("relayclient-int-%s", t.Name()))
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.MigrateRelay(ctx, db); err != nil {
		t.Fatalf("MigrateRelay: %v", err)
	}
	st := store.NewRelayStore(db)
	now := time.Unix(1_700_000_000, 0).UTC()
	srv := relayd.NewServer(st,
		relayd.WithClock(&testutil.FakeClock{T: now}),
		relayd.WithAdminToken("admin-secret"),
		relayd.WithVersion("test-v"),
	)
	// Seed client + projects directly in the store.
	if err := st.CreateClient(ctx, clientID, token, "test-pc", now); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	for _, p := range projects {
		if err := st.BindProject(ctx, p, clientID, now); err != nil {
			t.Fatalf("BindProject %s: %v", p, err)
		}
	}
	hs := httptest.NewServer(srv.Routes())
	t.Cleanup(hs.Close)
	return srv, hs
}

// freshPCStore mirrors internal/store/pc_test.go's helper; pinned in this
// integration test file to keep the relayclient package free of test-only
// store helpers.
func freshPCStore(t *testing.T) *store.PCStore {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenInMemory(fmt.Sprintf("pc-int-%s", t.Name()))
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.MigratePC(ctx, db); err != nil {
		t.Fatalf("MigratePC: %v", err)
	}
	return store.NewPCStore(db)
}

// wsURL converts the relay hs http[s]:// URL to a ws[s]:// tunnel URL.
func wsURL(hs *httptest.Server) string {
	u, _ := url.Parse(hs.URL)
	u.Scheme = "ws"
	u.Path = "/tunnel"
	return u.String()
}

// TestPhase3_HappyPath is the symmetric mirror of Phase 2's tunnel test:
// the full relayd + relayclient contract across two in-memory SQLite DBs.
//
// Steps:
//  1. relayd seeded with c1/project-a
//  2. relayclient.Run() dials, sends HELLO{last_seqs:0}
//  3. external POST to relay /project-a (ingress)
//  4. relayd stores webhook seq=1, pushes via WebSocket to PC
//  5. PC stores it in its local SQLite
//  6. PC sends ACK{up_to_seq:1}
//  7. relayd's projects.acked_seq advances to 1
func TestPhase3_HappyPath(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	relaySrv, hs := freshRelayServer(t, "c1", "secret-token", "project-a")
	pcStore := freshPCStore(t)

	webhooks := make(chan store.WebhookRow, 8)
	c := relayclient.New(relayclient.Config{
		URL:         wsURL(hs),
		ClientID:    "c1",
		ClientToken: "secret-token",
		Projects:    []string{"project-a"},
	}, pcStore,
		relayclient.WithClock(&testutil.FakeClock{T: time.Unix(1_700_000_000, 0).UTC()}),
		relayclient.WithCallbacks(relayclient.Callbacks{
			OnWebhook: func(r store.WebhookRow) { webhooks <- r },
		}),
	)
	go func() { _ = c.Run(ctx) }()

	// Ingress the webhook via HTTP — the third-party sender's path.
	body := []byte(`{"event":"order.created","id":42}`)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, hs.URL+"/project-a/orders/42", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "push")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ingress: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ingress status = %d", resp.StatusCode)
	}

	// Wait for the PC to receive + persist the webhook.
	select {
	case got := <-webhooks:
		if got.Project != "project-a" || got.Seq != 1 {
			t.Errorf("OnWebhook = %+v, want project-a/seq=1", got)
		}
		if !bytes.Equal(got.Body, body) {
			t.Errorf("OnWebhook body = %q, want %q", got.Body, body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for PC to receive webhook")
	}

	// Confirm local SQLite has the row.
	if got, _ := pcStore.LastSeq(ctx, "project-a"); got != 1 {
		t.Errorf("PCStore.LastSeq = %d, want 1", got)
	}

	// Confirm the relay's acked_seq advanced to 1 (with retry because the
	// relay's read goroutine processes the ACK asynchronously).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if seq, _ := relaySrv.AckedSeqThroughStore(ctx, "project-a"); seq == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if seq, _ := relaySrv.AckedSeqThroughStore(ctx, "project-a"); seq != 1 {
		t.Fatalf("relay acked_seq = %d, want 1", seq)
	}
}

// TestPhase3_OfflineIngressStreamsOnConnect confirms store-and-forward:
// when webhooks arrive at the relay's HTTP ingress while NO tunnel is
// attached, the relay stores them in its SQLite. When the PC then connects
// and HELLOs with last_seqs.project-a=0, the relay's HandleTunnel pumps
// every row with seq > 0 down the wire. The PC stores + acks; acked_seq
// advances to the number of offline webhooks.
//
// This is the contract that makes the relay survive PC downtime — the
// whole point of the ack-cursor pattern.
func TestPhase3_OfflineIngressStreamsOnConnect(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	relaySrv, hs := freshRelayServer(t, "c1", "secret-token", "project-a")
	pcStore := freshPCStore(t)

	fixedTime := time.Unix(1_700_000_000, 0).UTC()

	// 1. Ingress two webhooks BEFORE any tunnel is attached.
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, hs.URL+"/project-a",
			bytes.NewReader([]byte(fmt.Sprintf(`{"i":%d}`, i))))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("offline ingress[%d]: %v", i, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("offline ingress[%d] status = %d", i, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	// Confirm the relay has both rows sitting undelivered.
	if got, _ := relaySrv.PendingCountThroughStore(ctx, "project-a"); got != 2 {
		t.Fatalf("relay pending = %d, want 2 before tunnel attaches", got)
	}

	// 2. Start the relayclient; it HELLOs with last_seqs=0.
	arrivals := make(chan store.WebhookRow, 8)
	c := relayclient.New(relayclient.Config{
		URL:         wsURL(hs),
		ClientID:    "c1",
		ClientToken: "secret-token",
		Projects:    []string{"project-a"},
	}, pcStore,
		relayclient.WithClock(&testutil.FakeClock{T: fixedTime}),
		relayclient.WithCallbacks(relayclient.Callbacks{
			OnWebhook: func(r store.WebhookRow) { arrivals <- r },
		}),
	)
	go func() { _ = c.Run(ctx) }()

	// 3. Wait for both offline webhooks to stream into the PC.
	for i := 0; i < 2; i++ {
		select {
		case got := <-arrivals:
			if got.Project != "project-a" {
				t.Errorf("arrival[%d] project = %q, want project-a", i, got.Project)
			}
			if got.Seq != int64(i+1) {
				t.Errorf("arrival[%d] seq = %d, want %d", i, got.Seq, i+1)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for offline webhook %d", i+1)
		}
	}

	// 4. PC store should have both rows.
	if got, _ := pcStore.LastSeq(ctx, "project-a"); got != 2 {
		t.Errorf("PC LastSeq = %d, want 2", got)
	}

	// 5. relay's acked_seq should advance to 2 (with async retry).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if seq, _ := relaySrv.AckedSeqThroughStore(ctx, "project-a"); seq == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if seq, _ := relaySrv.AckedSeqThroughStore(ctx, "project-a"); seq != 2 {
		t.Fatalf("relay acked_seq = %d, want 2", seq)
	}

	_ = sync.Mutex{} // keep sync import used in case tinker removes
}
