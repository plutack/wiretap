package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
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
