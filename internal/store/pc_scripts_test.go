package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// sampleScript is a compact ScriptRow builder for tests.
func sampleScript(name, trigger string, priority int, enabled bool) ScriptRow {
	return ScriptRow{
		Name:     name,
		Trigger:  trigger,
		Body:     "request.body = 'x';",
		Priority: priority,
		Enabled:  enabled,
	}
}

func TestPCStore_InsertScript_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)

	id, err := s.InsertScript(ctx, sampleScript("sign", "on_request", 10, true), fixedTime)
	if err != nil {
		t.Fatalf("InsertScript: %v", err)
	}
	if id <= 0 {
		t.Fatalf("id = %d, want > 0", id)
	}
	got, err := s.ScriptByID(ctx, id)
	if err != nil {
		t.Fatalf("ScriptByID: %v", err)
	}
	if got.Name != "sign" || got.Trigger != "on_request" || got.Priority != 10 || !got.Enabled {
		t.Errorf("script = %+v", got)
	}
	if got.Body != "request.body = 'x';" {
		t.Errorf("body = %q", got.Body)
	}
	if !got.CreatedAt.Equal(fixedTime) || !got.UpdatedAt.Equal(fixedTime) {
		t.Errorf("timestamps = %v / %v, want %v", got.CreatedAt, got.UpdatedAt, fixedTime)
	}
}

func TestPCStore_ScriptByID_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	if _, err := s.ScriptByID(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestPCStore_UpdateScript(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	id, err := s.InsertScript(ctx, sampleScript("a", "on_request", 1, true), fixedTime)
	if err != nil {
		t.Fatalf("InsertScript: %v", err)
	}
	later := fixedTime.Add(time.Hour)
	err = s.UpdateScript(ctx, ScriptRow{
		ID: id, Name: "b", Trigger: "on_response", Body: "response.status = 204;", Priority: 5, Enabled: false,
	}, later)
	if err != nil {
		t.Fatalf("UpdateScript: %v", err)
	}
	got, err := s.ScriptByID(ctx, id)
	if err != nil {
		t.Fatalf("ScriptByID: %v", err)
	}
	if got.Name != "b" || got.Trigger != "on_response" || got.Priority != 5 || got.Enabled {
		t.Errorf("script = %+v", got)
	}
	if !got.CreatedAt.Equal(fixedTime) {
		t.Errorf("CreatedAt = %v, want unchanged %v", got.CreatedAt, fixedTime)
	}
	if !got.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, later)
	}
}

func TestPCStore_UpdateScript_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	err := s.UpdateScript(ctx, ScriptRow{ID: 42, Name: "x", Trigger: "on_request"}, fixedTime)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestPCStore_SetScriptEnabled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	id, err := s.InsertScript(ctx, sampleScript("a", "on_request", 1, true), fixedTime)
	if err != nil {
		t.Fatalf("InsertScript: %v", err)
	}
	if err := s.SetScriptEnabled(ctx, id, false, fixedTime.Add(time.Minute)); err != nil {
		t.Fatalf("SetScriptEnabled: %v", err)
	}
	got, err := s.ScriptByID(ctx, id)
	if err != nil {
		t.Fatalf("ScriptByID: %v", err)
	}
	if got.Enabled {
		t.Errorf("enabled = true, want false")
	}
	if err := s.SetScriptEnabled(ctx, 999, true, fixedTime); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing id: err = %v, want ErrNotFound", err)
	}
}

func TestPCStore_DeleteScript(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	id, err := s.InsertScript(ctx, sampleScript("a", "on_request", 1, true), fixedTime)
	if err != nil {
		t.Fatalf("InsertScript: %v", err)
	}
	if err := s.DeleteScript(ctx, id); err != nil {
		t.Fatalf("DeleteScript: %v", err)
	}
	if _, err := s.ScriptByID(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteScript(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double delete: err = %v, want ErrNotFound", err)
	}
}

func TestPCStore_Scripts_OrderedByTriggerThenPriority(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	// Insert out of order.
	for _, sc := range []ScriptRow{
		sampleScript("resp-hi", "on_response", 20, true),
		sampleScript("req-lo", "on_request", 5, true),
		sampleScript("resp-lo", "on_response", 10, true),
		sampleScript("req-hi", "on_request", 15, false),
	} {
		if _, err := s.InsertScript(ctx, sc, fixedTime); err != nil {
			t.Fatalf("InsertScript %s: %v", sc.Name, err)
		}
	}
	all, err := s.Scripts(ctx)
	if err != nil {
		t.Fatalf("Scripts: %v", err)
	}
	gotOrder := make([]string, len(all))
	for i, sc := range all {
		gotOrder[i] = sc.Name
	}
	// on_request before on_response (alpha); within each, priority ascending.
	want := []string{"req-lo", "req-hi", "resp-lo", "resp-hi"}
	if len(gotOrder) != len(want) {
		t.Fatalf("got %v, want %v", gotOrder, want)
	}
	for i := range want {
		if gotOrder[i] != want[i] {
			t.Fatalf("order = %v, want %v", gotOrder, want)
		}
	}
}

func TestPCStore_ScriptSummaries_OmitBodies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	if _, err := s.InsertScript(ctx, sampleScript("sign", "on_request", 10, true), fixedTime); err != nil {
		t.Fatalf("InsertScript: %v", err)
	}

	rows, err := s.ScriptSummaries(ctx)
	if err != nil {
		t.Fatalf("ScriptSummaries: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "sign" || rows[0].Body != "" {
		t.Fatalf("summaries = %+v", rows)
	}
	full, err := s.ScriptByID(ctx, rows[0].ID)
	if err != nil {
		t.Fatalf("ScriptByID: %v", err)
	}
	if full.Body == "" {
		t.Fatal("full script body was lost")
	}
}

func TestPCStore_ScriptsByTrigger_EnabledFilter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	for _, sc := range []ScriptRow{
		sampleScript("on", "on_webhook", 2, true),
		sampleScript("off", "on_webhook", 1, false),
		sampleScript("other", "on_request", 1, true),
	} {
		if _, err := s.InsertScript(ctx, sc, fixedTime); err != nil {
			t.Fatalf("InsertScript %s: %v", sc.Name, err)
		}
	}

	enabled, err := s.ScriptsByTrigger(ctx, "on_webhook", true)
	if err != nil {
		t.Fatalf("ScriptsByTrigger enabled: %v", err)
	}
	if len(enabled) != 1 || enabled[0].Name != "on" {
		t.Fatalf("enabled = %+v, want just [on]", enabled)
	}

	all, err := s.ScriptsByTrigger(ctx, "on_webhook", false)
	if err != nil {
		t.Fatalf("ScriptsByTrigger all: %v", err)
	}
	// Priority ascending: off (1) before on (2).
	if len(all) != 2 || all[0].Name != "off" || all[1].Name != "on" {
		t.Fatalf("all = %+v, want [off, on]", all)
	}
}
