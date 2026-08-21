package tui

import (
	"context"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/plutack/wiretap/internal/store"
)

// replayModel is the one-line replay prompt: a textinput prefilled with the
// configured forward URL (the GUI prefill behavior), targeting one webhook.
// The root model owns submission — it needs Deps.Replay, which lives there.

type replayModel struct {
	input textinput.Model
	wh    *store.WebhookRow
}

type replayResultMsg struct {
	project string
	seq     int64
	target  string
	status  int
	err     error
}

func newReplay(wh *store.WebhookRow, forwardURL string, width int) replayModel {
	ti := textinput.New()
	ti.Placeholder = "http://localhost:3000/webhook"
	ti.Prompt = "replay " + wh.Project + "/" + strconv.FormatInt(wh.Seq, 10) + " → "
	ti.PromptStyle = currentTheme.accent
	ti.SetValue(forwardURL)
	ti.Width = maxInt(width-4, 20)
	ti.Focus()
	return replayModel{input: ti, wh: wh}
}

func (r replayModel) Update(msg tea.Msg) (replayModel, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		// Enter/escape are handled by the root model (submit/cancel); the
		// input only ever sees editing keys.
		switch k.String() {
		case "enter", "esc":
			return r, nil
		}
	}
	var cmd tea.Cmd
	r.input, cmd = r.input.Update(msg)
	return r, cmd
}

func (r replayModel) View() string {
	return r.input.View()
}

// replayCmd builds the async replay command. on_replay transforms run inside
// app.App.ReplayWebhook; a script reject surfaces as its reason.
func replayCmd(deps Deps, project string, seq int64, target string) tea.Cmd {
	return func() tea.Msg {
		status, err := deps.Replay(context.Background(), project, seq, target)
		return replayResultMsg{project: project, seq: seq, target: target, status: status, err: err}
	}
}

// stripURL is a tiny sanitizer so an empty or whitespace-only prompt never
// becomes a confusing request.
func stripURL(s string) string { return strings.TrimSpace(s) }
