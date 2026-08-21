// Package tui implements the `wiretap tui` Bubbletea dashboard: a live feed
// of captured webhooks (ingress tab), intercepted traffic with session
// filtering (traffic tab), and transform scripts with enable toggles
// (transforms tab) — the terminal counterpart of the Wails GUI, reading the
// same app.App composition root through the Deps seam in deps.go.
//
// The model polls the store at a 500ms interval (decoupled from the relay
// lifecycle, and historical data survives restarts); slower-moving state
// (status snapshot, scripts, sessions) refreshes every 5s, the same split
// the GUI's pollers use. Selection, fuzzy filtering, and vim-style
// navigation come from bubbles lists, so every list accepts j/k/g/G, "/",
// and paging by default.
package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/plutack/wiretap/internal/store"
)

// tab identifies the three dashboard tabs, mirroring the GUI's Ingress and
// Traffic tabs plus the transforms sidebar promoted to a tab.
type tab int

const (
	tabIngress tab = iota
	tabTraffic
	tabTransforms
)

// focus routes key handling; exactly one pane owns the keyboard at a time.
type focus int

const (
	focusLists focus = iota
	focusDetail
	focusReplay
	focusExport
)

// methodCycles and statusCycles back the lens filters (the GUI sidebar's
// method + status-family dropdowns). "" means "all".
var (
	methodCycle  = []string{"", "GET", "POST", "PUT", "PATCH", "DELETE"}
	statusCycle  = []string{"", "2xx", "3xx", "4xx", "5xx"}
	rowLimit     = 100
	sessionLimit = 50
)

// slowEvery refreshes status/scripts/sessions every N fast ticks (500ms x 10
// = 5s, matching the GUI's secondary polling cadence).
const slowEvery = 10

// Model is the Bubbletea state for the wiretap TUI.
type Model struct {
	deps Deps

	tab   tab
	focus focus

	ingress list.Model
	traffic list.Model
	scripts list.Model

	// Latest raw snapshots; filters re-derive list items from these without
	// a refetch, and the paused backlog diff uses them too.
	webhookRows []store.WebhookRow
	captureRows []store.TrafficCaptureRow
	scriptRows  []store.ScriptRow
	sessions    []store.InterceptSessionRow

	paused     bool
	pausedKeys map[string]struct{} // identity of rows frozen at pause time
	backlog    int                 // unseen rows on the active tab while paused
	sessionIdx int                 // 0 = all sessions, else index+1 into sessions
	methodLens string              // "" = all methods
	statusLens string              // "" = all statuses

	detail *detailModel
	replay *replayModel
	export *exportModel

	toast    string
	toastErr bool
	toastAt  time.Time

	status    StatusSnapshot
	slowTicks int

	// Sticky export selection, mirroring the GUI's remembered dropdowns.
	lastTarget string
	lastClient string

	width    int
	height   int
	err      error
	lastPoll time.Time
}

// Option configures a Model.
type Option func(*Model)

// WithTheme selects the palette by config name ("dark"/"light"); unknown
// names fall back to dark. This finally wires the tui.theme config field,
// which the GUI settings screen has been writing all along.
func WithTheme(name string) Option {
	return func(m *Model) { currentTheme = themeFor(name) }
}

// New builds a Model over the given Deps. Lists are sized on the first
// WindowSizeMsg; data lands on the first tick (Init fires one immediately).
func New(deps Deps, opts ...Option) Model {
	m := Model{deps: deps}
	m.ingress = newList(nil, renderWebhookRow, 0, 3)
	m.traffic = newList(nil, renderCaptureRow, 0, 3)
	m.scripts = newList(nil, renderScriptRow, 0, 3)
	for _, o := range opts {
		o(&m)
	}
	return m
}

// tickMsg drives periodic refreshes. 500ms is responsive enough for webhook
// development without hammering SQLite.
type tickMsg struct{}

// Init fires an immediate tick (so data shows without waiting 500ms) and
// arms the tick loop.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg { return tickMsg{} },
		tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} }),
	)
}

// Update handles message dispatch.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tickMsg:
		cmd := m.refresh(context.Background())
		return m, tea.Batch(cmd,
			tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
				return tickMsg{}
			}))
	case replayResultMsg:
		if msg.err != nil {
			m.setToast(fmt.Sprintf("replay %s/%d failed: %v", msg.project, msg.seq, msg.err), true)
		} else {
			m.setToast(fmt.Sprintf("replayed %s/%d → %s (HTTP %d)", msg.project, msg.seq, msg.target, msg.status), false)
		}
		return m, nil
	case exportTargetsMsg:
		if msg.err != nil {
			m.setToast("export targets unavailable: "+msg.err.Error(), true)
			m.export = nil
			m.focus = focusLists
			return m, nil
		}
		if m.export != nil {
			m.export.targets = msg.targets
			m.export.stage = exportPickTarget
			for i, t := range msg.targets { // sticky preselect, GUI parity
				if t.Key == m.lastTarget {
					m.export.targetCursor = i
					break
				}
			}
		}
		return m, nil
	case exportSnippetMsg:
		if m.export != nil {
			m.export.snippet = msg.snippet
			m.export.snipErr = msg.err
			m.export.vp.SetContent(msg.snippet)
			m.export.vp.GotoTop()
		}
		if msg.err != nil {
			m.setToast("export failed: "+msg.err.Error(), true)
		}
		return m, nil
	case copiedMsg:
		m.setToast(fmt.Sprintf("copied %s (OSC 52)", byteCount(msg.bytes)), false)
		return m, nil
	case scriptToggleMsg:
		if msg.err != nil {
			m.setToast(fmt.Sprintf("toggle %s failed: %v", msg.name, msg.err), true)
		} else {
			state := "disabled"
			if msg.enabled {
				state = "enabled"
			}
			m.setToast(msg.name+" "+state, false)
		}
		return m, m.refreshScripts(context.Background())
	}
	// Pass unhandled messages down to the focused pane. The lists need this
	// too: bubbles filtering and pagination deliver their own internal
	// messages (FilterMatchesMsg and friends) here, and dropping them would
	// leave "/" filters permanently stale.
	var cmd tea.Cmd
	switch m.focus {
	case focusLists:
		return m.updateActiveList(msg)
	case focusDetail:
		if m.detail != nil {
			*m.detail, cmd = m.detail.Update(msg)
		}
	case focusExport:
		if m.export != nil {
			*m.export, cmd = m.export.Update(msg)
		}
	case focusReplay:
		if m.replay != nil {
			*m.replay, cmd = m.replay.Update(msg)
		}
	}
	return m, cmd
}

// scriptToggleMsg reports the outcome of a space-bar toggle on the
// transforms tab.
type scriptToggleMsg struct {
	id      int64
	name    string
	enabled bool
	err     error
}

// handleKey routes one keypress by focus.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.focus {
	case focusReplay:
		return m.handleReplayKey(msg)
	case focusExport:
		return m.handleExportKey(msg)
	case focusDetail:
		return m.handleDetailKey(msg)
	default:
		return m.handleListKey(msg)
	}
}

func (m Model) handleReplayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.replay = nil
		m.focus = focusLists
		m.layout()
		return m, nil
	case "enter":
		r := m.replay
		m.replay = nil
		m.focus = focusLists
		m.layout()
		if r == nil {
			return m, nil
		}
		target := stripURL(r.input.Value())
		if target == "" {
			m.setToast("replay canceled: no target URL", true)
			return m, nil
		}
		m.setToast("replaying "+r.wh.Project+"/"+strconv.FormatInt(r.wh.Seq, 10)+" → "+target+"…", false)
		return m, replayCmd(m.deps, r.wh.Project, r.wh.Seq, target)
	}
	var cmd tea.Cmd
	*m.replay, cmd = m.replay.Update(msg)
	return m, cmd
}

func (m Model) handleExportKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.export
	if e == nil {
		m.focus = focusLists
		return m, nil
	}
	switch msg.String() {
	case "esc":
		switch e.stage {
		case exportPickClient:
			e.stage = exportPickTarget
			return m, nil
		case exportShowSnippet:
			e.stage = exportPickClient
			e.snippet, e.snipErr = "", nil
			e.vp.SetContent("")
			return m, nil
		default:
			m.export = nil
			m.focus = focusLists
			m.layout()
			return m, nil
		}
	case "enter":
		switch e.stage {
		case exportPickTarget:
			e.stage = exportPickClient
			e.clientCursor = 0
			if t := e.target(); t.Key == m.lastTarget && m.lastClient != "" {
				for i, c := range t.Clients {
					if c.Key == m.lastClient {
						e.clientCursor = i + 1
						break
					}
				}
			}
			return m, nil
		case exportPickClient:
			t := e.target()
			m.lastTarget, m.lastClient = t.Key, e.client()
			e.stage = exportShowSnippet
			e.snippet = "generating snippet…"
			e.vp.SetContent(e.snippet)
			return m, exportSnippetCmd(m.deps, *e, t.Key, e.client())
		}
		return m, nil
	}
	var cmd tea.Cmd
	*m.export, cmd = m.export.Update(msg)
	return m, cmd
}

func (m Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		m.detail = nil
		m.focus = focusLists
		m.layout()
		return m, nil
	}
	if m.detail == nil {
		m.focus = focusLists
		return m, nil
	}
	switch msg.String() {
	case "r":
		if wh := m.detail.selectedWebhook(); wh != nil {
			r := newReplay(wh, m.status.ForwardURL, m.width)
			m.replay = &r
			m.focus = focusReplay
			m.layout()
			return m, r.input.Focus()
		}
		return m, nil
	case "e":
		m.openExportFromDetail()
		return m, fetchTargetsCmd(m.deps)
	case "y":
		if body := m.detailBody(); body != "" {
			return m, copyTextCmd(body)
		}
		return m, nil
	}
	var cmd tea.Cmd
	*m.detail, cmd = m.detail.Update(msg)
	return m, cmd
}

func (m Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	active := m.activeList()
	// While a list's filter input has focus, every key (including q and the
	// lens keys) must reach the input as text.
	if active.FilterState() == list.Filtering {
		return m.updateActiveList(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		// No global escape at list level: esc clears an applied filter
		// inside the list; otherwise it is a no-op (quit stays on q).
		return m.updateActiveList(msg)
	case "tab":
		m.switchTab((m.tab + 1) % 3)
		return m, nil
	case "shift+tab":
		m.switchTab((m.tab + 2) % 3)
		return m, nil
	case "1":
		m.switchTab(tabIngress)
		return m, nil
	case "2":
		m.switchTab(tabTraffic)
		return m, nil
	case "3":
		m.switchTab(tabTransforms)
		return m, nil
	case "p":
		if m.tab != tabTransforms {
			return m, m.togglePause()
		}
		return m, nil
	case "f":
		if m.tab != tabTransforms {
			return m, m.cycleMethodLens()
		}
		return m, nil
	case "F":
		if m.tab == tabTraffic {
			return m, m.cycleStatusLens()
		}
		return m, nil
	case "S":
		if m.tab == tabTraffic {
			m.cycleSession()
		}
		return m, nil
	case "c":
		return m, m.clearLens()
	case "enter":
		return m.openDetail()
	case "r":
		if m.tab == tabIngress {
			if it, ok := m.ingress.SelectedItem().(webhookItem); ok {
				r := newReplay(&it.row, m.status.ForwardURL, m.width)
				m.replay = &r
				m.focus = focusReplay
				m.layout()
				return m, r.input.Focus()
			}
		}
		return m, nil
	case "e":
		eh := maxInt(m.contentHeight()+1, 4)
		switch m.tab {
		case tabIngress:
			if it, ok := m.ingress.SelectedItem().(webhookItem); ok {
				e := newExport(&it.row, nil, m.width, eh)
				m.export = &e
				m.focus = focusExport
				m.layout()
				return m, fetchTargetsCmd(m.deps)
			}
		case tabTraffic:
			if it, ok := m.traffic.SelectedItem().(captureItem); ok {
				e := newExport(nil, &it.row, m.width, eh)
				m.export = &e
				m.focus = focusExport
				m.layout()
				return m, fetchTargetsCmd(m.deps)
			}
		}
		return m, nil
	case "y":
		switch m.tab {
		case tabIngress:
			if it, ok := m.ingress.SelectedItem().(webhookItem); ok && len(it.row.Body) > 0 {
				return m, copyTextCmd(string(it.row.Body))
			}
		case tabTraffic:
			if it, ok := m.traffic.SelectedItem().(captureItem); ok && len(it.row.RespBody) > 0 {
				return m, copyTextCmd(string(it.row.RespBody))
			}
		}
		return m, nil
	case " ":
		if m.tab == tabTransforms {
			return m.toggleScript()
		}
		return m, nil
	}
	return m.updateActiveList(msg)
}

// updateActiveList forwards a message to the focused tab's list.
func (m Model) updateActiveList(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.tab {
	case tabIngress:
		m.ingress, cmd = m.ingress.Update(msg)
	case tabTraffic:
		m.traffic, cmd = m.traffic.Update(msg)
	case tabTransforms:
		m.scripts, cmd = m.scripts.Update(msg)
	}
	return m, cmd
}

func (m *Model) activeList() *list.Model {
	switch m.tab {
	case tabIngress:
		return &m.ingress
	case tabTraffic:
		return &m.traffic
	default:
		return &m.scripts
	}
}

func (m *Model) switchTab(t tab) { m.tab = t }

func (m *Model) togglePause() tea.Cmd {
	if m.paused {
		m.paused = false
		m.backlog = 0
		m.pausedKeys = nil
		return m.applyRows()
	}
	m.paused = true
	m.pausedKeys = map[string]struct{}{}
	switch m.tab {
	case tabIngress:
		for _, r := range m.webhookRows {
			m.pausedKeys[webhookItem{row: r}.key()] = struct{}{}
		}
	case tabTraffic:
		for _, r := range m.captureRows {
			m.pausedKeys[captureItem{row: r}.key()] = struct{}{}
		}
	}
	m.backlog = 0
	return nil
}

func (m *Model) cycleMethodLens() tea.Cmd {
	for i, v := range methodCycle {
		if v == m.methodLens {
			m.methodLens = methodCycle[(i+1)%len(methodCycle)]
			break
		}
	}
	return m.applyRows()
}

func (m *Model) cycleStatusLens() tea.Cmd {
	for i, v := range statusCycle {
		if v == m.statusLens {
			m.statusLens = statusCycle[(i+1)%len(statusCycle)]
			break
		}
	}
	return m.applyRows()
}

// cycleSession rotates the traffic tab's session filter: all → newest → …
// → all. Changing the session refetches captures through the query itself.
func (m *Model) cycleSession() {
	m.sessionIdx = (m.sessionIdx + 1) % (len(m.sessions) + 1)
	m.applySession()
	m.refreshCaptures(context.Background())
}

func (m *Model) applySession() {
	if m.sessionIdx == 0 || m.sessionIdx > len(m.sessions) {
		m.sessionIdx = 0
	}
}

func (m *Model) sessionID() int64 {
	if m.sessionIdx == 0 {
		return 0
	}
	return m.sessions[m.sessionIdx-1].ID
}

func (m *Model) clearLens() tea.Cmd {
	m.methodLens, m.statusLens = "", ""
	m.sessionIdx = 0
	m.refreshCaptures(context.Background())
	return m.applyRows()
}

func (m Model) openDetail() (tea.Model, tea.Cmd) {
	// +1: detail keeps its own hint line in addition to the list-area rows.
	h := maxInt(m.contentHeight()+1, 4)
	switch m.tab {
	case tabIngress:
		if it, ok := m.ingress.SelectedItem().(webhookItem); ok {
			d := newWebhookDetail(&it.row, m.width, h)
			m.detail = &d
			m.focus = focusDetail
			m.layout()
		}
	case tabTraffic:
		if it, ok := m.traffic.SelectedItem().(captureItem); ok {
			d := newCaptureDetail(&it.row, m.width, h)
			m.detail = &d
			m.focus = focusDetail
			m.layout()
		}
	}
	return m, nil
}

func (m *Model) openExportFromDetail() {
	if m.detail == nil {
		return
	}
	e := newExport(m.detail.selectedWebhook(), m.detail.selectedCapture(), m.width, maxInt(m.contentHeight()+1, 4))
	m.export = &e
	m.focus = focusExport
	m.layout()
}

func (m Model) detailBody() string {
	if m.detail == nil {
		return ""
	}
	if wh := m.detail.selectedWebhook(); wh != nil {
		return string(wh.Body)
	}
	if c := m.detail.selectedCapture(); c != nil {
		return string(c.RespBody)
	}
	return ""
}

func (m Model) toggleScript() (tea.Model, tea.Cmd) {
	it, ok := m.scripts.SelectedItem().(scriptItem)
	if !ok {
		return m, nil
	}
	id, name := it.row.ID, it.row.Name
	enable := !it.row.Enabled
	deps := m.deps
	return m, func() tea.Msg {
		err := deps.SetScriptEnabled(context.Background(), id, enable)
		return scriptToggleMsg{id: id, name: name, enabled: enable, err: err}
	}
}

// --- refresh --------------------------------------------------------------

// refresh reads the latest rows and re-derives list items. The active tab's
// list is frozen while paused (with a backlog count), matching the GUI's
// follow/pause semantics. Returns a command that refreshes text filters.
func (m *Model) refresh(ctx context.Context) tea.Cmd {
	m.lastPoll = time.Now()
	m.slowTicks++

	if m.deps.Webhooks != nil {
		if rows, err := m.deps.Webhooks(ctx, "", rowLimit); err == nil {
			m.webhookRows = rows
			m.err = nil
		} else {
			m.err = err
		}
	}
	m.refreshCaptures(ctx)

	var cmds []tea.Cmd
	if m.slowTicks%slowEvery == 1 || len(m.sessions) == 0 {
		if m.deps.Sessions != nil {
			if rows, err := m.deps.Sessions(ctx, sessionLimit); err == nil {
				m.sessions = rows
				m.applySession()
			}
		}
		cmds = append(cmds, m.refreshScripts(ctx))
		if m.deps.Status != nil {
			m.status = m.deps.Status()
		}
	}

	cmds = append(cmds, m.applyRows())
	return tea.Batch(cmds...)
}

func (m *Model) refreshCaptures(ctx context.Context) {
	if m.deps.CapturesBySession == nil {
		return
	}
	rows, err := m.deps.CapturesBySession(ctx, m.sessionID(), rowLimit)
	if err != nil {
		m.err = err
		return
	}
	m.captureRows = rows
}

func (m *Model) refreshScripts(ctx context.Context) tea.Cmd {
	if m.deps.Scripts == nil {
		return nil
	}
	rows, err := m.deps.Scripts(ctx)
	if err != nil {
		return nil
	}
	m.scriptRows = rows
	return setItems(&m.scripts, scriptItems(m.scriptRows))
}

// applyRows rebuilds the ingress/traffic list items from the latest
// snapshots through the lens filters, preserving the cursor by row identity.
// The active tab is skipped while paused so its rows stay frozen. The
// returned command re-runs any active text filter (see setItems).
func (m *Model) applyRows() tea.Cmd {
	var cmds []tea.Cmd
	if !(m.paused && m.tab == tabIngress) {
		cmds = append(cmds, setItems(&m.ingress, webhookItems(m.webhookRows, m.methodLens)))
	}
	if !(m.paused && m.tab == tabTraffic) {
		cmds = append(cmds, setItems(&m.traffic, captureItems(m.captureRows, m.methodLens, m.statusLens)))
	}
	if m.paused && m.tab != tabTransforms {
		m.backlog = m.countBacklog()
	}
	return tea.Batch(cmds...)
}

// countBacklog counts fresh rows on the active tab that were not in the
// frozen snapshot — the TUI twin of the GUI's "N new events" pill.
func (m *Model) countBacklog() int {
	if m.pausedKeys == nil {
		return 0
	}
	count := 0
	switch m.tab {
	case tabIngress:
		for _, r := range m.webhookRows {
			if _, seen := m.pausedKeys[webhookItem{row: r}.key()]; !seen {
				count++
			}
		}
	case tabTraffic:
		for _, r := range m.captureRows {
			if _, seen := m.pausedKeys[captureItem{row: r}.key()]; !seen {
				count++
			}
		}
	}
	return count
}

// setItems swaps a list's rows while pinning the cursor by row identity.
// The returned command must be executed: bubbles clears its filtered matches
// on SetItems and re-filters asynchronously, so dropping it would wipe any
// active "/" filter on every refresh tick.
func setItems(l *list.Model, items []list.Item) tea.Cmd {
	key := selectedKey(*l)
	cmd := l.SetItems(items)
	restoreCursor(l, key)
	return cmd
}

func webhookItems(rows []store.WebhookRow, method string) []list.Item {
	out := make([]list.Item, 0, len(rows))
	for _, r := range rows {
		if method != "" && r.Method != method {
			continue
		}
		out = append(out, webhookItem{row: r})
	}
	return out
}

func captureItems(rows []store.TrafficCaptureRow, method, status string) []list.Item {
	out := make([]list.Item, 0, len(rows))
	for _, r := range rows {
		if method != "" && r.Method != method {
			continue
		}
		if status != "" && !statusInFamily(r.Status, status) {
			continue
		}
		out = append(out, captureItem{row: r})
	}
	return out
}

func statusInFamily(status int, family string) bool {
	switch family {
	case "2xx":
		return status >= 200 && status < 300
	case "3xx":
		return status >= 300 && status < 400
	case "4xx":
		return status >= 400 && status < 500
	case "5xx":
		return status >= 500 && status < 600
	}
	return false
}

func scriptItems(rows []store.ScriptRow) []list.Item {
	out := make([]list.Item, 0, len(rows))
	for _, r := range rows {
		out = append(out, scriptItem{row: r})
	}
	return out
}

// --- layout & view ---------------------------------------------------------

func (m *Model) layout() {
	if m.width == 0 {
		return
	}
	ch := m.contentHeight()
	m.ingress.SetSize(m.width, maxInt(ch, 3))
	m.traffic.SetSize(m.width, maxInt(ch, 3))
	m.scripts.SetSize(m.width, maxInt(ch, 3))
	if m.detail != nil {
		m.detail.resize(m.width, maxInt(ch+1, 4))
	}
	if m.export != nil {
		m.export.resize(m.width, maxInt(ch+1, 4))
	}
}

// contentHeight is the rows available to list/detail/export panes: total
// minus header, tabs, rule, status, and help lines; one less when the
// replay prompt is open.
func (m Model) contentHeight() int {
	if m.height == 0 {
		return 3
	}
	h := m.height - 5
	if m.replay != nil {
		h--
	}
	return maxInt(h, 3)
}

func (m *Model) setToast(s string, isErr bool) {
	m.toast, m.toastErr, m.toastAt = s, isErr, time.Now()
}

// toastTTL bounds how long an action result stays on the status line.
const toastTTL = 4 * time.Second

func (m Model) toastText() (string, bool) {
	if m.toast == "" || time.Since(m.toastAt) > toastTTL {
		return "", false
	}
	return m.toast, m.toastErr
}

// View renders the dashboard.
func (m Model) View() string {
	if m.width == 0 {
		return "Loading...\n"
	}
	var b strings.Builder

	b.WriteString(m.headerLine())
	b.WriteByte('\n')
	b.WriteString(m.tabLine())
	b.WriteByte('\n')
	b.WriteString(m.rule())
	b.WriteByte('\n')

	switch m.focus {
	case focusExport:
		if m.export != nil {
			b.WriteString(m.export.View())
		}
	case focusDetail:
		if m.detail != nil {
			b.WriteString(m.detail.View())
		}
	default:
		b.WriteString(m.activeList().View())
	}

	if m.replay != nil {
		b.WriteByte('\n')
		b.WriteString(m.replay.View())
	}
	b.WriteByte('\n')
	b.WriteString(m.statusLine())
	b.WriteByte('\n')
	b.WriteString(m.helpLine())
	return b.String()
}

func (m Model) headerLine() string {
	t := currentTheme
	parts := []string{t.title.Render("wiretap")}
	if m.status.Version != "" {
		v := m.status.Version
		if v[0] >= '0' && v[0] <= '9' { // "1.2.3" → "v1.2.3"; "dev" stays "dev"
			v = "v" + v
		}
		parts = append(parts, t.dim.Render(v))
	}
	if m.status.RelayURL != "" {
		state := t.warn.Render("idle")
		if m.status.TunnelRunning {
			state = t.success.Render("connected")
		}
		relay := m.status.RelayURL
		if len(relay) > 42 {
			relay = relay[:39] + "…"
		}
		parts = append(parts, t.dim.Render(relay), state)
	} else {
		parts = append(parts, t.dim.Render("relay not configured"))
	}
	if len(m.status.ConnectedProjects) > 0 {
		parts = append(parts, t.dim.Render("watching "+strings.Join(m.status.ConnectedProjects, ",")))
	}
	return strings.Join(parts, " · ")
}

func (m Model) tabLine() string {
	t := currentTheme
	name := func(n tab, label string, count int) string {
		s := fmt.Sprintf("%s (%d)", label, count)
		if m.tab == n {
			return t.accent.Render("▶ " + s)
		}
		return t.dim.Render("  " + s)
	}
	parts := []string{
		name(tabIngress, "Ingress", len(m.webhookRows)),
		name(tabTraffic, "Traffic", len(m.captureRows)),
		name(tabTransforms, "Transforms", len(m.scriptRows)),
	}
	if m.paused {
		mark := t.warn.Render("paused")
		if m.backlog > 0 {
			mark = t.warn.Render(fmt.Sprintf("paused · %d new", m.backlog))
		}
		parts = append(parts, mark)
	}
	return strings.Join(parts, "   ")
}

func (m Model) rule() string {
	return currentTheme.dim.Render(strings.Repeat("─", maxInt(m.width-1, 1)))
}

func (m Model) statusLine() string {
	t := currentTheme
	if s, isErr := m.toastText(); s != "" {
		style := t.success
		if isErr {
			style = t.error_
		}
		return style.Render(cutWidth(s, maxInt(m.width-1, 1)))
	}
	parts := []string{}
	if m.methodLens != "" {
		parts = append(parts, "method "+m.methodLens)
	}
	if m.statusLens != "" {
		parts = append(parts, "status "+m.statusLens)
	}
	if m.sessionIdx > 0 && m.sessionIdx <= len(m.sessions) {
		s := m.sessions[m.sessionIdx-1]
		parts = append(parts, fmt.Sprintf("session #%d (%d captures)", s.ID, s.Captures))
	}
	if m.err != nil {
		parts = append(parts, "error: "+m.err.Error())
	} else {
		parts = append(parts, m.lastPoll.Local().Format("updated 15:04:05"))
	}
	return t.dim.Render(cutWidth(strings.Join(parts, " · "), maxInt(m.width-1, 1)))
}

func (m Model) helpLine() string {
	if m.focus != focusLists {
		return currentTheme.dim.Render("") // panes draw their own hints
	}
	var h string
	switch m.tab {
	case tabIngress:
		h = "↵ detail · / search · f method · p pause · r replay · e export · y copy body · 1/2/3 tabs · q quit"
	case tabTraffic:
		h = "↵ detail · / search · f method · F status · S session · p pause · e export · y copy resp · 1/2/3 tabs · q quit"
	case tabTransforms:
		h = "space toggle · / search · 1/2/3 tabs · q quit"
	}
	return currentTheme.dim.Render(cutWidth(h, maxInt(m.width-1, 1)))
}
