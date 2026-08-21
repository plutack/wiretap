package tui

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/plutack/wiretap/internal/store"
)

// This file ports the GUI's three row types (webhook-list, traffic-list,
// transforms sidebar) to selectable bubbles lists with vim-friendly keys.
// Rows carry the full store row so the detail pane renders without a
// refetch; FilterValue mirrors what the GUI's client-side search matches.

// currentTheme is the palette the row delegates render with. bubbles lists
// carry no custom payload, so it is package-level and set once per program
// run in New; the TUI never re-themes at runtime.
var currentTheme = darkTheme()

// --- items ----------------------------------------------------------------

type webhookItem struct{ row store.WebhookRow }

func (i webhookItem) FilterValue() string {
	return i.row.Project + " " + i.row.Method + " " + i.row.Path
}

// key identifies the row across refreshes so the cursor can be restored.
func (i webhookItem) key() string { return i.row.Project + "/" + strconv.FormatInt(i.row.Seq, 10) }

type captureItem struct{ row store.TrafficCaptureRow }

func (i captureItem) FilterValue() string {
	return fmt.Sprintf("%s %d %s", i.row.Method, i.row.Status, i.row.URL)
}

func (i captureItem) key() string { return strconv.FormatInt(i.row.ID, 10) }

type scriptItem struct{ row store.ScriptRow }

func (i scriptItem) FilterValue() string {
	return i.row.Name + " " + i.row.Trigger
}

func (i scriptItem) key() string { return strconv.FormatInt(i.row.ID, 10) }

// selectedKey extracts the stable identity of the currently selected item so
// the cursor survives a 500ms refresh that shifts every index.
func selectedKey(l list.Model) string {
	switch it := l.SelectedItem().(type) {
	case webhookItem:
		return it.key()
	case captureItem:
		return it.key()
	case scriptItem:
		return it.key()
	}
	return ""
}

// restoreCursor re-selects the item whose key matches want, keeping the
// bubble on new arrivals instead of sliding off the row the user picked.
// Scans the visible (post-filter) items so a text filter cannot hide the
// cursor.
func restoreCursor(l *list.Model, want string) {
	if want == "" {
		return
	}
	for idx, it := range l.VisibleItems() {
		var k string
		switch v := it.(type) {
		case webhookItem:
			k = v.key()
		case captureItem:
			k = v.key()
		case scriptItem:
			k = v.key()
		}
		if k == want {
			l.Select(idx)
			return
		}
	}
}

// --- delegate -------------------------------------------------------------

// rowDelegate renders any item as one padded, colorized line. Column layout
// lives in the per-type render funcs below, mirroring the GUI list columns.
type rowDelegate struct {
	render func(w io.Writer, item list.Item, selected bool, width int, t theme)
}

func (d rowDelegate) Height() int                             { return 1 }
func (d rowDelegate) Spacing() int                            { return 0 }
func (d rowDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d rowDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	selected := index == m.Index() && m.FilterState() != list.Filtering
	d.render(w, item, selected, m.Width()-2, currentTheme)
}

// decorateRow prefixes the selection marker and, for the selected row, pads
// to the full width so the cursor background spans the terminal — without
// the padding a subtle background only covers the text, which is nearly
// impossible to see on dark terminals.
func decorateRow(line string, selected bool, width int, t theme) string {
	if !selected {
		return "  " + line
	}
	line = "❯ " + line
	if pad := width - lipgloss.Width(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return t.cursor.Render(line)
}

// padRight right-pads s with spaces to want cells (display-width aware).
func padRight(s string, want int) string {
	gap := want - lipgloss.Width(s)
	if gap <= 0 {
		return s
	}
	return s + strings.Repeat(" ", gap)
}

// cutWidth truncates s to want display cells, appending "…" when cut.
func cutWidth(s string, want int) string {
	if want <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= want {
		return s
	}
	if want == 1 {
		return ansi.Truncate(s, 1, "")
	}
	return ansi.Truncate(s, want-1, "…")
}

func renderWebhookRow(w io.Writer, item list.Item, selected bool, width int, t theme) {
	r, ok := item.(webhookItem)
	if !ok {
		return
	}
	method := t.method.Render(padRight(cutWidth(r.row.Method, 7), 7))
	src := t.badge.Render(padRight(cutWidth(fmt.Sprintf("%s/%d", r.row.Project, r.row.Seq), 18), 18))
	path := cutWidth(r.row.Path, maxInt(width-2-7-18-10-9, 4))
	size := padRight(byteCount(len(r.row.Body)), 9)
	line := strings.Join([]string{method, src, path, size, fmtTime(r.row.ReceivedAt)}, "  ")
	fmt.Fprint(w, decorateRow(line, selected, width, t))
}

func renderCaptureRow(w io.Writer, item list.Item, selected bool, width int, t theme) {
	r, ok := item.(captureItem)
	if !ok {
		return
	}
	method := t.method.Render(padRight(cutWidth(r.row.Method, 7), 7))
	status := padRight("–", 4)
	if r.row.Status != 0 {
		status = padRight(t.statusStyle(r.row.Status).Render(strconv.Itoa(r.row.Status)), 4)
	}
	xfer := padRight(fmt.Sprintf("↑%s ↓%s", byteCount(len(r.row.ReqBody)), byteCount(len(r.row.RespBody))), 17)
	url := cutWidth(r.row.URL, maxInt(width-2-7-4-17-9, 4))
	line := strings.Join([]string{method, status, url, xfer, fmtTime(r.row.At)}, "  ")
	fmt.Fprint(w, decorateRow(line, selected, width, t))
}

func renderScriptRow(w io.Writer, item list.Item, selected bool, width int, t theme) {
	r, ok := item.(scriptItem)
	if !ok {
		return
	}
	mark := t.success.Render("✔")
	name := r.row.Name
	if !r.row.Enabled {
		mark = t.dim.Render("✗")
		name = t.dim.Render(name)
	}
	// Trigger is stored as the full "on_replay"-style string already.
	meta := t.dim.Render(fmt.Sprintf("%s · prio %d", r.row.Trigger, r.row.Priority))
	line := fmt.Sprintf("%s %s  %s", mark, cutWidth(name, maxInt(width-2-30, 8)), meta)
	fmt.Fprint(w, decorateRow(line, selected, width, t))
}

// --- list construction ----------------------------------------------------

// newList builds a list with the dashboard defaults: no chrome of its own
// except the built-in "/" filter bar (input + match count, so typing is
// visible), pagination dots kept, and the default vim keymap minus the keys
// the dashboard reclaims (f/d/u default to paging there).
func newList(items []list.Item, render func(io.Writer, list.Item, bool, int, theme), width, height int) list.Model {
	l := list.New(items, rowDelegate{render: render}, width, maxInt(height, 3))
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.DisableQuitKeybindings()
	l.KeyMap.PrevPage = key.NewBinding(key.WithKeys("left", "h", "pgup", "b"), key.WithHelp("←/h", "prev page"))
	l.KeyMap.NextPage = key.NewBinding(key.WithKeys("right", "l", "pgdown"), key.WithHelp("→/l", "next page"))
	return l
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// fmtTime is the shared local-time column format.
func fmtTime(t time.Time) string { return t.Local().Format("15:04:05") }
