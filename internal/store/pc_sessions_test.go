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
	// Migrations must stay re-runnable; the ledger makes this second call a
	// no-op while older databases still tolerate the already-added column.
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

func TestReconcileInterceptSessions_ClosesOnlyCrashedSessions(t *testing.T) {
	t.Parallel()
	s := newSessionTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	old := now.Add(-2 * time.Hour)

	// Crashed run that captured traffic: the last capture is when it stopped.
	crashed, err := s.CreateInterceptSession(ctx, old, "bash", ":0")
	if err != nil {
		t.Fatalf("create crashed: %v", err)
	}
	lastCapture := old.Add(7 * time.Minute)
	for _, at := range []time.Time{old.Add(time.Minute), lastCapture} {
		if _, err := s.InsertTrafficCapture(ctx, TrafficCaptureRow{
			SessionID: crashed, At: at, Method: "GET", URL: "https://x.test/",
		}); err != nil {
			t.Fatalf("insert capture: %v", err)
		}
	}

	// Crashed run that captured nothing: fall back to started_at.
	emptyStarted := old.Add(-time.Hour)
	empty, err := s.CreateInterceptSession(ctx, emptyStarted, "fish", ":0")
	if err != nil {
		t.Fatalf("create empty: %v", err)
	}

	// Owned by a live process, so it must be left alone.
	active, err := s.CreateInterceptSession(ctx, old.Add(-time.Hour), "bash", ":0")
	if err != nil {
		t.Fatalf("create active: %v", err)
	}

	// Started moments ago: inside the grace period, so left alone.
	recent, err := s.CreateInterceptSession(ctx, now.Add(-time.Second), "bash", ":0")
	if err != nil {
		t.Fatalf("create recent: %v", err)
	}

	// Already closed cleanly.
	closedCleanly, err := s.CreateInterceptSession(ctx, old.Add(-time.Hour), "bash", ":0")
	if err != nil {
		t.Fatalf("create closed: %v", err)
	}
	cleanEnd := old.Add(-30 * time.Minute)
	if err := s.EndInterceptSession(ctx, closedCleanly, cleanEnd); err != nil {
		t.Fatalf("EndInterceptSession: %v", err)
	}

	n, err := s.ReconcileInterceptSessions(ctx, active, now, time.Minute)
	if err != nil {
		t.Fatalf("ReconcileInterceptSessions: %v", err)
	}
	if n != 2 {
		t.Errorf("closed %d sessions, want 2", n)
	}

	rows, err := s.InterceptSessions(ctx, 10)
	if err != nil {
		t.Fatalf("InterceptSessions: %v", err)
	}
	byID := make(map[int64]InterceptSessionRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}

	if got := byID[crashed]; !got.Interrupted || got.EndedAt.Unix() != lastCapture.Unix() {
		t.Errorf("crashed = interrupted %v, ended %v; want true, %v", got.Interrupted, got.EndedAt, lastCapture)
	}
	if got := byID[empty]; !got.Interrupted || got.EndedAt.Unix() != emptyStarted.Unix() {
		t.Errorf("empty = interrupted %v, ended %v; want true, %v", got.Interrupted, got.EndedAt, emptyStarted)
	}
	if got := byID[active]; got.Interrupted || !got.EndedAt.IsZero() {
		t.Errorf("active = interrupted %v, ended %v; want false, zero", got.Interrupted, got.EndedAt)
	}
	if got := byID[recent]; got.Interrupted || !got.EndedAt.IsZero() {
		t.Errorf("recent = interrupted %v, ended %v; want false, zero", got.Interrupted, got.EndedAt)
	}
	if got := byID[closedCleanly]; got.Interrupted || got.EndedAt.Unix() != cleanEnd.Unix() {
		t.Errorf("closed = interrupted %v, ended %v; want false, %v", got.Interrupted, got.EndedAt, cleanEnd)
	}

	// Second run is a no-op: nothing is left open.
	again, err := s.ReconcileInterceptSessions(ctx, active, now, time.Minute)
	if err != nil {
		t.Fatalf("second ReconcileInterceptSessions: %v", err)
	}
	if again != 0 {
		t.Errorf("second run closed %d sessions, want 0", again)
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

	all := listCaptures(t, s, 0, 10)
	if len(all) != 2 {
		t.Fatalf("all = %d rows, want 2", len(all))
	}

	in := listCaptures(t, s, sid, 10)
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
