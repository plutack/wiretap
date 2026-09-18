package tui

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/plutack/wiretap/internal/store"
)

// runExecMsgs drives the model and executes returned commands a bounded
// number of levels deep, approximating bubbletea's runtime — filtering in
// bubbles lists happens inside commands (batched with cursor blinks), so a
// test that ignores or under-executes them sees stale matches. Unbounded
// chasing would never terminate on lists whose spinner reschedules itself.
func runExecMsgs(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	const maxDepth = 6
	cur := tea.Model(m)
	var exec func(cmd tea.Cmd, depth int)
	exec = func(cmd tea.Cmd, depth int) {
		if cmd == nil || depth >= maxDepth {
			return
		}
		msg := cmd()
		if msg == nil {
			return
		}
		if _, isTick := msg.(tickMsg); isTick {
			return // don't chase re-armed 500ms ticks; tests drive time explicitly
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				exec(c, depth+1)
			}
			return
		}
		var next tea.Cmd
		cur, next = cur.Update(msg)
		exec(next, depth+1)
	}
	for _, msg := range msgs {
		var cmd tea.Cmd
		cur, cmd = cur.Update(msg)
		exec(cmd, 0)
	}
	return cur.(Model)
}

// TestTrafficFilterQueriesTheStore pins that "/" narrows the query rather than
// the loaded page. The matching capture sits behind a row of newer traffic that
// fills the list's page, so a frontend-side filter could never surface it.
func TestTrafficFilterQueriesTheStore(t *testing.T) {
	st := freshPCStore(t)
	ctx := context.Background()
	if _, err := st.InsertTrafficCapture(ctx, store.TrafficCaptureRow{
		At: tuiFixedTime, Method: "POST", URL: "https://api.test/checkout", Status: 201,
	}); err != nil {
		t.Fatalf("InsertTrafficCapture: %v", err)
	}
	// Newer rows that match nothing, so the target is not the newest page.
	for i := range rowLimit {
		if _, err := st.InsertTrafficCapture(ctx, store.TrafficCaptureRow{
			At:     tuiFixedTime.Add(time.Duration(i+1) * time.Second),
			Method: "GET", URL: fmt.Sprintf("https://api.test/noise/%d", i), Status: 200,
		}); err != nil {
			t.Fatalf("InsertTrafficCapture noise %d: %v", i, err)
		}
	}

	m := mustTick(t, New(storeDeps(st)))
	m.tab = tabTraffic
	if len(m.captureRows) != rowLimit {
		t.Fatalf("unfiltered rows = %d, want a full page of %d", len(m.captureRows), rowLimit)
	}

	m.traffic.SetFilterText("checkout")
	out := runExecMsgs(t, m, tickMsg{})

	if len(out.captureRows) != 1 {
		t.Fatalf("filtered rows = %d, want 1", len(out.captureRows))
	}
	if got := out.captureRows[0].URL; got != "https://api.test/checkout" {
		t.Errorf("filtered row = %q, want the checkout capture", got)
	}
	// The status lens travels with it, so a lens that excludes the match empties
	// the result rather than falling back to the loaded page.
	out.methodLens = "GET"
	out = runExecMsgs(t, out, tickMsg{})
	if len(out.captureRows) != 0 {
		t.Errorf("rows with method lens GET = %d, want 0", len(out.captureRows))
	}
}

// TestTextFilterSurvivesRefresh pins the fix for the bug where every 500ms
// tick silently wiped an applied "/" filter: SetItems clears the matched
// items and returns a re-filter command that must be propagated. The filter
// is established via SetFilterText (synchronous, no cursor-blink sleeps);
// the tick then exercises the real production path.
func TestTextFilterSurvivesRefresh(t *testing.T) {
	st := freshPCStore(t)
	seedWebhook(t, st, "stripe", 1, "POST", "/pay", nil)
	seedWebhook(t, st, "github", 1, "GET", "/hook", nil)

	m := mustTick(t, New(storeDeps(st)))
	m.ingress.SetFilterText("stripe")
	if got := len(m.ingress.VisibleItems()); got != 1 {
		t.Fatalf("after SetFilterText: visible = %d, want 1", got)
	}

	out := runExecMsgs(t, m, tickMsg{})
	if got := len(out.ingress.VisibleItems()); got != 1 {
		t.Fatalf("after tick: visible = %d (state %v), want 1", got, out.ingress.FilterState())
	}

	// A resize relayout must not kill it either.
	out = runExecMsgs(t, out, tea.WindowSizeMsg{Width: 90, Height: 28})
	if got := len(out.ingress.VisibleItems()); got != 1 {
		t.Fatalf("after resize: visible = %d (state %v), want 1", got, out.ingress.FilterState())
	}
}

// TestFilterTypingDoesNotQuit guards the "everything is text while typing"
// rule: q, f, and friends must reach the filter input, not the dashboard.
// Only synchronous state is asserted, so no command execution is needed.
func TestFilterTypingDoesNotQuit(t *testing.T) {
	st := freshPCStore(t)
	seedWebhook(t, st, "p", 1, "POST", "/x", nil)

	m := mustTick(t, New(storeDeps(st)))
	out, _ := run(t, m, keyPress("/"), keyPress("q"))
	if out.ingress.FilterState() != list.Filtering {
		t.Fatalf("filter state = %v, want Filtering", out.ingress.FilterState())
	}
	if v := out.ingress.FilterInput.Value(); v != "q" {
		t.Errorf("filter input = %q, want q", v)
	}
}
