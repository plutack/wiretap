package store

import (
	"context"
	"testing"
	"time"
)

func newSessionTestStore(t *testing.T) *PCStore {
	t.Helper()
	db, err := OpenInMemory(t.Name())
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := MigratePC(context.Background(), db); err != nil {
		t.Fatalf("MigratePC: %v", err)
	}
	// Migrations must stay re-runnable (they replay at every startup); the
	// ALTER TABLE in 003 relies on the duplicate-column tolerance.
	if err := MigratePC(context.Background(), db); err != nil {
		t.Fatalf("MigratePC re-run: %v", err)
	}
	return NewPCStore(db)
}

func TestInterceptSessions_CreateEndList(t *testing.T) {
	t.Parallel()
	s := newSessionTestStore(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	id, err := s.CreateInterceptSession(ctx, start, "bash", "127.0.0.1:8888")
	if err != nil {
		t.Fatalf("CreateInterceptSession: %v", err)
	}
	if id == 0 {
		t.Fatal("session id = 0")
	}

	// Two captures in the session, one outside it.
	for _, sid := range []int64{id, id, 0} {
		if _, err := s.InsertTrafficCapture(ctx, TrafficCaptureRow{
			SessionID: sid, At: start, Method: "GET", URL: "https://x.test/",
		}); err != nil {
			t.Fatalf("InsertTrafficCapture: %v", err)
		}
	}

	sessions, err := s.InterceptSessions(ctx, 10)
	if err != nil {
		t.Fatalf("InterceptSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	got := sessions[0]
	if got.ID != id || got.Shell != "bash" || got.ProxyAddr != "127.0.0.1:8888" {
		t.Errorf("session row = %+v", got)
	}
	if !got.EndedAt.IsZero() {
		t.Errorf("EndedAt = %v, want zero while running", got.EndedAt)
	}
	if got.Captures != 2 {
		t.Errorf("Captures = %d, want 2", got.Captures)
	}

	end := start.Add(10 * time.Minute)
	if err := s.EndInterceptSession(ctx, id, end); err != nil {
		t.Fatalf("EndInterceptSession: %v", err)
	}
	sessions, _ = s.InterceptSessions(ctx, 10)
	if sessions[0].EndedAt.Unix() != end.Unix() {
		t.Errorf("EndedAt = %v, want %v", sessions[0].EndedAt, end)
	}
}

func TestInterceptSessionsPage_CursorAndTotal(t *testing.T) {
	t.Parallel()
	s := newSessionTestStore(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	var ids []int64
	for i := range 5 {
		id, err := s.CreateInterceptSession(ctx, start.Add(time.Duration(i)*time.Minute), "bash", ":0")
		if err != nil {
			t.Fatalf("CreateInterceptSession %d: %v", i, err)
		}
		ids = append(ids, id)
	}

	first, total, err := s.InterceptSessionsPage(ctx, 0, 2)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if total != 5 || len(first) != 2 || first[0].ID != ids[4] || first[1].ID != ids[3] {
		t.Fatalf("first = %+v, total = %d", first, total)
	}

	second, total, err := s.InterceptSessionsPage(ctx, first[1].ID, 2)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if total != 5 || len(second) != 2 || second[0].ID != ids[2] || second[1].ID != ids[1] {
		t.Fatalf("second = %+v, total = %d", second, total)
	}
}

func TestTrafficCapturesBySession_Filters(t *testing.T) {
	t.Parallel()
	s := newSessionTestStore(t)
	ctx := context.Background()
	now := time.Now()

	sid, err := s.CreateInterceptSession(ctx, now, "fish", ":0")
	if err != nil {
		t.Fatalf("CreateInterceptSession: %v", err)
	}
	if _, err := s.InsertTrafficCapture(ctx, TrafficCaptureRow{SessionID: sid, At: now, URL: "https://in.test/"}); err != nil {
		t.Fatalf("insert in-session: %v", err)
	}
	if _, err := s.InsertTrafficCapture(ctx, TrafficCaptureRow{At: now, URL: "https://out.test/"}); err != nil {
		t.Fatalf("insert unsessioned: %v", err)
	}

	all, err := s.TrafficCapturesBySession(ctx, 0, 10)
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("all = %d rows, want 2", len(all))
	}

	in, err := s.TrafficCapturesBySession(ctx, sid, 10)
	if err != nil {
		t.Fatalf("filtered: %v", err)
	}
	if len(in) != 1 || in[0].URL != "https://in.test/" || in[0].SessionID != sid {
		t.Errorf("filtered rows = %+v", in)
	}

	// Detail load keeps the session id too.
	got, err := s.TrafficCaptureByID(ctx, in[0].ID)
	if err != nil {
		t.Fatalf("TrafficCaptureByID: %v", err)
	}
	if got.SessionID != sid {
		t.Errorf("detail SessionID = %d, want %d", got.SessionID, sid)
	}
}
