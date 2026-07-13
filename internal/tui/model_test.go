package tui

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/store"
)

// freshPCStore stands up an in-memory migrated PCStore for TUI tests.
func freshPCStore(t *testing.T) *store.PCStore {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenInMemory(fmt.Sprintf("tui-%s", t.Name()))
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.MigratePC(ctx, db); err != nil {
		t.Fatalf("MigratePC: %v", err)
	}
	return store.NewPCStore(db)
}

var tuiFixedTime = time.Unix(1_700_000_000, 0).UTC()

func TestModel_Refresh_LoadsRows(t *testing.T) {
	st := freshPCStore(t)
	ctx := context.Background()

	// Seed 3 webhooks.
	for i := 0; i < 3; i++ {
		_, _ = st.StoreWebhook(ctx, store.WebhookRow{
			Project: "project-a", Seq: int64(i + 1),
			ReceivedAt: tuiFixedTime, Method: "POST", Path: "/x",
			HeadersJSON: "{}", Body: []byte("body"),
		}, tuiFixedTime)
	}

	m := New(st)
	m.refresh(ctx)

	if len(m.rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(m.rows))
	}
	// Newest-first (Webhooks returns ORDER BY seq DESC).
	if m.rows[0].Seq != 3 {
		t.Errorf("first row seq = %d, want 3 (newest-first)", m.rows[0].Seq)
	}
	if m.rows[0].Project != "project-a" {
		t.Errorf("project = %q", m.rows[0].Project)
	}
	if m.rows[0].BodyLen != 4 {
		t.Errorf("bodylen = %d, want 4", m.rows[0].BodyLen)
	}
}

func TestModel_Refresh_EmptyStore(t *testing.T) {
	st := freshPCStore(t)
	m := New(st)
	m.refresh(context.Background())

	if len(m.rows) != 0 {
		t.Errorf("rows = %d, want 0", len(m.rows))
	}
	if m.err != nil {
		t.Errorf("err = %v, want nil", m.err)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in     string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"", 5, ""},
		{"exactly10", 10, "exactly10"},
		{"too-long-string", 10, "too-long-…"},
	}
	for _, tc := range tests {
		got := truncate(tc.in, tc.maxLen)
		if got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.in, tc.maxLen, got, tc.want)
		}
	}
}

// TestView_RendersSomething confirms View produces non-empty output when
// rows exist. We don't assert exact strings (layout will change) but ensure
// it doesn't crash on a zero width.
func TestView_RendersSomething(t *testing.T) {
	st := freshPCStore(t)
	_, _ = st.StoreWebhook(context.Background(), store.WebhookRow{
		Project: "p", Seq: 1, ReceivedAt: tuiFixedTime,
		Method: "POST", Path: "/x", HeadersJSON: "{}", Body: []byte("b"),
	}, tuiFixedTime)

	m := New(st)
	m.refresh(context.Background())
	m.width = 80
	m.height = 24

	out := m.View()
	if out == "" {
		t.Fatal("View returned empty string")
	}
	if !contains(out, "wiretap") {
		t.Error("View should contain the word wiretap")
	}
}

// TestViewInit_SetsStore is a sanity check that New doesn't panic.
func TestNew_SetsStore(t *testing.T) {
	st := freshPCStore(t)
	m := New(st)
	if m.store == nil {
		t.Error("store should not be nil after New")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(s[:len(s)-len(sub)+1] != "" && containsHelper(s, sub)))
}

func containsHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
