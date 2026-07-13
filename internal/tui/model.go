// Package tui implements the `wiretap tui` Bubbletea dashboard. It shows a
// live feed of webhooks captured by the local relayclient (stored in
// PCStore's SQLite) plus a status bar with relay connection state.
//
// The model polls PCStore at a 500ms interval instead of subscribing to
// relayclient.Callbacks directly — this keeps the TUI decoupled from the
// relayclient lifecycle and lets users browse webhooks captured before
// the TUI started. A future improvement can wire OnWebhook as an event
// source for instant updates; the polling interval is a fine MVP.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/plutack/wiretap/internal/store"
)

// WebhookRow mirrors store.WebhookRow for display; we copy fields rather
// than importing store in the view path to keep the TUI easily testable
// with fakes.
type WebhookRow struct {
	Project string
	Seq     int64
	Method  string
	Path    string
	At      time.Time
	BodyLen int
}

// Model is the Bubbletea state for the wiretap TUI.
type Model struct {
	store    *store.PCStore
	rows     []WebhookRow
	width    int
	height   int
	err      error
	lastPoll time.Time
}

// New builds a Model backed by the given PCStore. The store is consulted on
// every tick to load the latest webhooks and on the initial View.
func New(s *store.PCStore) Model {
	return Model{store: s}
}

// pollMsg is sent on every poll interval to trigger a store read.
type pollMsg time.Time

// tickMsg drives periodic refreshes. 500ms is responsive enough for
// webhook development without hammering SQLite.
type tickMsg struct{}

// Init starts the tick loop.
func (m Model) Init() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

// Update handles message dispatch.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}
	case tickMsg:
		m.refresh(context.Background())
		return m, tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
			return tickMsg{}
		})
	case pollMsg:
		m.refresh(context.Background())
	}
	return m, nil
}

// refresh reads the latest webhooks from the store.
func (m *Model) refresh(ctx context.Context) {
	m.lastPoll = time.Now()
	rows, err := m.store.Webhooks(ctx, "", 100)
	if err != nil {
		m.err = err
		return
	}
	m.err = nil
	m.rows = make([]WebhookRow, 0, len(rows))
	for _, r := range rows {
		m.rows = append(m.rows, WebhookRow{
			Project: r.Project,
			Seq:     r.Seq,
			Method:  r.Method,
			Path:    r.Path,
			At:      r.ReceivedAt,
			BodyLen: len(r.Body),
		})
	}
}

// View renders the dashboard.
func (m Model) View() string {
	if m.width == 0 {
		return "Loading...\n"
	}

	var b strings.Builder

	// Title bar.
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("63")).
		Render("wiretap — webhook dashboard")
	b.WriteString(title)
	b.WriteByte('\n')

	// Status line.
	status := "ok"
	if m.err != nil {
		status = fmt.Sprintf("error: %v", m.err)
	}
	statusLine := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Render(fmt.Sprintf("%d webhooks  |  %s  |  press 'q' to quit",
			len(m.rows), status))
	b.WriteString(statusLine)
	b.WriteByte('\n')

	// Separator.
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteByte('\n')

	// Webhook table header.
	if len(m.rows) > 0 {
		header := fmt.Sprintf("%-12s  %-6s  %-8s  %-20s  %s",
			"Project", "Seq", "Method", "Path", "Body")
		b.WriteString(lipgloss.NewStyle().Bold(true).Render(header))
		b.WriteByte('\n')
	}

	// Webhook rows.
	for _, r := range m.rows {
		line := fmt.Sprintf("%-12s  %-6d  %-8s  %-20s  %d bytes",
			truncate(r.Project, 12), r.Seq, truncate(r.Method, 8),
			truncate(r.Path, 20), r.BodyLen)
		b.WriteString(line)
		b.WriteByte('\n')
	}

	if len(m.rows) == 0 && m.err == nil {
		b.WriteString("\n  No webhooks captured yet. POST to your relay's /<project>\n  endpoint to receive webhooks here.\n")
	}

	return b.String()
}

// truncate clamps s to maxLen characters, appending "…" when truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}
