package tui

import (
	"context"
	"fmt"
	"os"
	"strconv"

	osc52 "github.com/aymanbagabas/go-osc52/v2"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/plutack/wiretap/internal/export"
	"github.com/plutack/wiretap/internal/store"
)

// exportModel is the "export as code" flow, the TUI twin of the GUI's
// export-snippet panel: pick a language target, pick a client variant, then
// view the snippet in a scrollable viewport with `y` to copy via OSC 52.
// The root model remembers the last target/client and preselects them,
// mirroring the GUI's sticky dropdown selection.

type exportStage int

const (
	exportLoading exportStage = iota
	exportPickTarget
	exportPickClient
	exportShowSnippet
)

type exportModel struct {
	stage   exportStage
	targets []export.Target
	err     error

	// Simple cursor + rendered lines; the catalogs are tiny (≈9 targets,
	// ≈6 clients) so a bubbles list is overkill.
	targetCursor int
	clientCursor int

	snippet string
	snipErr error
	vp      viewport.Model
	width   int
	height  int

	wh  *store.WebhookRow // exactly one of these is set
	cap *store.TrafficCaptureRow
}

type exportTargetsMsg struct {
	targets []export.Target
	err     error
}

type exportSnippetMsg struct {
	snippet string
	err     error
}

func newExport(wh *store.WebhookRow, cap *store.TrafficCaptureRow, width, height int) exportModel {
	e := exportModel{stage: exportLoading, wh: wh, cap: cap}
	e.resize(width, height)
	return e
}

func (e *exportModel) resize(width, height int) {
	e.width, e.height = width, height
	e.vp = viewport.New(maxInt(width, 10), maxInt(height-2, 3))
	e.vp.SetContent(e.snippet)
}

// target returns the target currently under the cursor.
func (e exportModel) target() export.Target {
	if e.targetCursor < len(e.targets) {
		return e.targets[e.targetCursor]
	}
	return export.Target{}
}

// client returns the client key under the cursor ("" = target default).
func (e exportModel) client() string {
	t := e.target()
	if e.clientCursor == 0 || e.clientCursor > len(t.Clients) {
		return "" // default client
	}
	return t.Clients[e.clientCursor-1].Key
}

// title describes what is being exported.
func (e exportModel) title() string {
	if e.wh != nil {
		return fmt.Sprintf("export webhook %s/%d", e.wh.Project, e.wh.Seq)
	}
	return "export capture #" + strconv.FormatInt(e.cap.ID, 10)
}

func fetchTargetsCmd(deps Deps) tea.Cmd {
	return func() tea.Msg {
		targets, err := deps.ExportTargets()
		return exportTargetsMsg{targets: targets, err: err}
	}
}

func exportSnippetCmd(deps Deps, e exportModel, targetKey, clientKey string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var (
			snippet string
			err     error
		)
		if e.wh != nil {
			snippet, err = deps.ExportWebhook(ctx, e.wh.Project, e.wh.Seq, targetKey, clientKey)
		} else {
			snippet, err = deps.ExportCapture(ctx, e.cap.ID, targetKey, clientKey)
		}
		return exportSnippetMsg{snippet: snippet, err: err}
	}
}

// copyTextCmd pushes s to the terminal clipboard using OSC 52. It is
// best-effort: terminals without OSC 52 support silently ignore the escape
// sequence, so the toast claims nothing beyond "sent".
func copyTextCmd(s string) tea.Cmd {
	return func() tea.Msg {
		_, _ = osc52.New(s).WriteTo(os.Stdout)
		return copiedMsg{bytes: len(s)}
	}
}

type copiedMsg struct{ bytes int }

func (e exportModel) Update(msg tea.Msg) (exportModel, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "up", "k":
			switch e.stage {
			case exportPickTarget:
				if e.targetCursor > 0 {
					e.targetCursor--
				}
			case exportPickClient:
				if e.clientCursor > 0 {
					e.clientCursor--
				}
			case exportShowSnippet:
				e.vp.LineUp(1)
			}
			return e, nil
		case "down", "j":
			switch e.stage {
			case exportPickTarget:
				if e.targetCursor < len(e.targets)-1 {
					e.targetCursor++
				}
			case exportPickClient:
				if e.clientCursor < len(e.target().Clients) {
					e.clientCursor++
				}
			case exportShowSnippet:
				e.vp.LineDown(1)
			}
			return e, nil
		case "g", "home":
			if e.stage == exportShowSnippet {
				e.vp.GotoTop()
			} else {
				e.targetCursor, e.clientCursor = 0, 0
			}
			return e, nil
		case "G", "end":
			if e.stage == exportShowSnippet {
				e.vp.GotoBottom()
			} else if e.stage == exportPickTarget && len(e.targets) > 0 {
				e.targetCursor = len(e.targets) - 1
			} else if e.stage == exportPickClient {
				e.clientCursor = len(e.target().Clients)
			}
			return e, nil
		case "y":
			if e.stage == exportShowSnippet && e.snippet != "" {
				return e, copyTextCmd(e.snippet)
			}
		}
	}
	var cmd tea.Cmd
	e.vp, cmd = e.vp.Update(msg)
	return e, cmd
}

func (e exportModel) View() string {
	var b []byte
	out := func(format string, args ...any) { b = append(b, fmt.Sprintf(format, args...)...) }

	out("%s\n", currentTheme.title.Render(e.title()))
	switch e.stage {
	case exportLoading:
		out("%s\n", currentTheme.dim.Render("loading export targets… (esc cancels)"))
	case exportPickTarget:
		out("\n%s\n", currentTheme.accent.Render("Language / target:"))
		for i, t := range e.targets {
			e.renderChoice(&b, i == e.targetCursor, fmt.Sprintf("%s (%s)", t.Title, t.Key))
		}
		out("\n%s\n", currentTheme.dim.Render("↑/↓ or j/k select · enter confirm · esc cancel"))
	case exportPickClient:
		t := e.target()
		out("\n%s\n", currentTheme.accent.Render("Client for "+t.Title+":"))
		e.renderChoice(&b, e.clientCursor == 0, t.DefaultClient+"  (default)")
		for i, c := range t.Clients {
			e.renderChoice(&b, e.clientCursor == i+1, c.Title+"  ("+c.Key+")")
		}
		out("\n%s\n", currentTheme.dim.Render("↑/↓ or j/k select · enter confirm · esc back"))
	case exportShowSnippet:
		if e.snipErr != nil {
			out("%s\n", currentTheme.error_.Render("export failed: "+e.snipErr.Error()))
		} else {
			out("%s\n", e.vp.View())
		}
		out("%s\n", currentTheme.dim.Render("y copy (OSC 52) · j/k scroll · esc back"))
	}
	if e.err != nil {
		out("%s\n", currentTheme.error_.Render(e.err.Error()))
	}
	return string(b)
}

func (e exportModel) renderChoice(b *[]byte, selected bool, label string) {
	if selected {
		*b = append(*b, currentTheme.cursor.Render("  ▸ "+label)...)
	} else {
		*b = append(*b, currentTheme.dim.Render("    "+label)...)
	}
	*b = append(*b, '\n')
}
