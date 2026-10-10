package relayd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/relayproto"
	"github.com/plutack/wiretap/internal/store"
)

func TestCommittedWebhooksResumeAfterRelayRestart(t *testing.T) {
	path := t.TempDir() + "/relay.db"
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := store.MigrateRelay(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := NewServer(store.NewRelayStore(db))
	makeClientFor(t, s, "client", "token", "project-a")
	hs := httptest.NewServer(s.Routes())
	defer hs.Close()
	for range 2 {
		resp, err := http.Post(hs.URL+"/project-a", "text/plain", nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatal(resp.StatusCode)
		}
	}
	conn := dialTunnel(t, hs, "client", "token")
	writeTunnel(t, ctx, conn, relayproto.Hello{Base: relayproto.Base{Type: relayproto.TypeHello}, ClientID: "client"})
	_ = readTunnel(t, ctx, conn)
	if push, ok := readTunnel(t, ctx, conn).(relayproto.Push); !ok || push.Seq != 1 {
		t.Fatal(push)
	}
	// No ACK: shutdown must not discard committed/unacknowledged rows.
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	_ = conn.CloseNow()
	hs.Close()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	restarted := NewServer(store.NewRelayStore(db))
	next := httptest.NewServer(restarted.Routes())
	defer next.Close()
	resumed := dialTunnel(t, next, "client", "token")
	writeTunnel(t, ctx, resumed, relayproto.Hello{Base: relayproto.Base{Type: relayproto.TypeHello}, ClientID: "client"})
	_ = readTunnel(t, ctx, resumed)
	for seq := int64(1); seq <= 2; seq++ {
		push, ok := readTunnel(t, ctx, resumed).(relayproto.Push)
		if !ok || push.Seq != seq {
			t.Fatalf("resume: %#v", push)
		}
	}
	if err := restarted.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownJoinsTunnelAndRejectsNewWork(t *testing.T) {
	s, hs, _ := freshServer(t)
	makeClientFor(t, s, "client", "token", "project-a")
	conn := dialTunnel(t, hs, "client", "token")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	writeTunnel(t, ctx, conn, relayproto.Hello{Base: relayproto.Base{Type: relayproto.TypeHello}, ClientID: "client"})
	if _, ok := readTunnel(t, ctx, conn).(relayproto.OK); !ok {
		t.Fatal("expected greeting")
	}
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if got := s.tunnels.countTunnels(); got != 0 {
		t.Fatal("tunnel registry not drained", got)
	}
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, httptest.NewRequest("POST", "/project-a", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal(w.Code)
	}
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("tunnel left open")
	}
}

func TestShutdownClosesTunnelWaitingForHello(t *testing.T) {
	s, hs, _ := freshServer(t)
	makeClientFor(t, s, "client", "token", "project-a")
	conn := dialTunnel(t, hs, "client", "token")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("hello waiter not stopped")
	}
}

func TestDrainAllowsAcceptedHandlerAndForceCancels(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "drain", true: "force"}[force], func(t *testing.T) {
			s, _, _ := freshServer(t)
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			handler := s.admit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				select {
				case <-release:
				case <-r.Context().Done():
				}
				w.WriteHeader(200)
			}))
			go func() {
				defer close(done)
				handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/project-a", nil))
			}()
			<-entered
			s.BeginDrain()
			if !force {
				close(release)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			err := s.Shutdown(ctx)
			if force && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			if !force && err != nil {
				t.Fatal(err)
			}
			joinCtx, joinCancel := context.WithTimeout(context.Background(), time.Second)
			defer joinCancel()
			if err := s.Wait(joinCtx); err != nil {
				t.Fatal(err)
			}
			<-done
		})
	}
}
