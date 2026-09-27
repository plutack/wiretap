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

type webhookItem struct{ row store.WebhookSummaryRow }

func (i webhookItem) FilterValue() string {
	return i.row.Project + " " + i.row.Method + " " + i.row.Path
}

// key identifies the row across refreshes so the cursor can be restored.
func (i webhookItem) key() string { return i.row.Project + "/" + strconv.FormatInt(i.row.Seq, 10) }

type captureItem struct {
	row store.TrafficCaptureSummaryRow
}

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

// padLeft left-pads s with spaces to want cells. Used for numeric and time
// columns so their digits line up on the right edge.
func padLeft(s string, want int) string {
	gap := want - lipgloss.Width(s)
	if gap <= 0 {
		return s
	}
	return strings.Repeat(" ", gap) + s
}

// cellStyle renders a padded cell. lipgloss.Style.Render has exactly this
// shape, so a style can be assigned directly and applied after padding.
type cellStyle func(strs ...string) string

// colSpec is one cell of a list table. width == 0 marks a flexible column that
// absorbs the space left after the fixed ones; right aligns a column to its
// right edge. style is applied after the cell is padded, so colouring never
// disturbs the layout.
type colSpec struct {
	width int
	right bool
	text  string
	style cellStyle
}

// renderCols lays the cells out on one line of exactly width display cells.
//
// Every cell is padded to its final width — including the flexible one. That is
// what keeps the columns in the same place no matter how long a URL or path is:
// a cell that is merely truncated (and never padded) lets everything after it
// slide left, so the size and time columns wander from row to row.
func renderCols(cells []colSpec, width int) string {
	if len(cells) == 0 || width <= 0 {
		return ""
	}
	const sep = "  "
	flexible, fixed := 0, 0
	for _, c := range cells {
		if c.width <= 0 {
			flexible++
			continue
		}
		fixed += c.width
	}
	slack := width - fixed - len(sep)*(len(cells)-1)
	flexWidth, remainder := 0, 0
	if flexible > 0 {
		flexWidth, remainder = slack/flexible, slack%flexible
	}

	var b strings.Builder
	for _, c := range cells {
		if b.Len() > 0 {
			b.WriteString(sep)
		}
		w := c.width
		if w <= 0 {
			w = flexWidth
			if remainder > 0 {
				w++
				remainder--
			}
			if w < 1 {
				w = 1
			}
		}
		text := cutWidth(c.text, w)
		if c.right {
			text = padLeft(text, w)
		} else {
			text = padRight(text, w)
		}
		if c.style != nil {
			text = c.style(text)
		}
		b.WriteString(text)
	}
	return b.String()
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

// Column widths shared by the rows and their header, so labels can never drift
// out of alignment with the data beneath them.
const (
	colMethodW = 6
	colStatusW = 4
	colSourceW = 18
	colXferW   = 17
	colSizeW   = 9
	colTimeW   = 8
)

// webhookColumns is the ingress table's layout. ROUTE absorbs the slack, which
// pushes PAYLOAD and SEEN to fixed positions at the right edge.
func webhookColumns(r store.WebhookSummaryRow, t theme) []colSpec {
	return []colSpec{
		{width: colMethodW, text: r.Method, style: t.method.Render},
		{width: colSourceW, text: fmt.Sprintf("%s/%d", r.Project, r.Seq), style: t.badge.Render},
		{text: r.Path},
		{width: colSizeW, right: true, text: byteCount(r.BodyLength)},
		{width: colTimeW, right: true, text: fmtTime(r.ReceivedAt), style: t.dim.Render},
	}
}

// captureColumns is the traffic table's layout; the URL absorbs the slack.
func captureColumns(r store.TrafficCaptureSummaryRow, t theme) []colSpec {
	status := "–"
	var statusStyle cellStyle
	if r.Status != 0 {
		status = strconv.Itoa(r.Status)
		style := t.statusStyle(r.Status)
		statusStyle = style.Render
	}
	return []colSpec{
		{width: colMethodW, text: r.Method, style: t.method.Render},
		{width: colStatusW, text: status, style: statusStyle},
		{text: r.URL},
		{width: colXferW, right: true, text: fmt.Sprintf("↑%s ↓%s", byteCount(r.ReqBodyLen), byteCount(r.RespBodyLen))},
		{width: colTimeW, right: true, text: fmtTime(r.At), style: t.dim.Render},
	}
}

// listHeader labels the active tab's columns. It renders through renderCols
// with the same widths as the rows, so it lines up exactly.
func listHeader(tab tab, width int, t theme) string {
	dim := t.dim.Render
	switch tab {
	case tabIngress:
		return renderCols([]colSpec{
			{width: colMethodW, text: "METHOD", style: dim},
			{width: colSourceW, text: "SOURCE", style: dim},
			{text: "ROUTE", style: dim},
			{width: colSizeW, right: true, text: "PAYLOAD", style: dim},
			{width: colTimeW, right: true, text: "SEEN", style: dim},
		}, width)
	case tabTraffic:
		return renderCols([]colSpec{
			{width: colMethodW, text: "METHOD", style: dim},
			{width: colStatusW, text: "CODE", style: dim},
			{text: "URL", style: dim},
			{width: colXferW, right: true, text: "TRANSFER", style: dim},
			{width: colTimeW, right: true, text: "SEEN", style: dim},
		}, width)
	default:
		return ""
	}
}

// hasColumns reports whether a tab is rendered as a table (and therefore needs
// the header line).
func hasColumns(tab tab) bool { return tab == tabIngress || tab == tabTraffic }

func renderWebhookRow(w io.Writer, item list.Item, selected bool, width int, t theme) {
	r, ok := item.(webhookItem)
	if !ok {
		return
	}
	fmt.Fprint(w, decorateRow(renderCols(webhookColumns(r.row, t), width), selected, width, t))
}

func renderCaptureRow(w io.Writer, item list.Item, selected bool, width int, t theme) {
	r, ok := item.(captureItem)
	if !ok {
		return
	}
	fmt.Fprint(w, decorateRow(renderCols(captureColumns(r.row, t), width), selected, width, t))
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
