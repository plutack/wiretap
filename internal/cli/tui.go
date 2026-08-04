package cli

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/plutack/wiretap/internal/app"
	"github.com/plutack/wiretap/internal/tui"
)

// newTUICmd builds the `wiretap tui` command. It uses the same *app.App
// composition root as the GUI: Open the local store, start the relay tunnel
// in the background, and poll the store every 500ms for the dashboard.
//
// Switching from the old newPCStore/startTunnelBackground pair to *app.App
// means the TUI now gets:
//   - the same default-path resolution for wiretap.db as the GUI
//   - the same tunnel OnConnect wiring, so the dashboard can show which
//     projects the relay actually says this client owns
//
// app.App owns the lifecycle; the TUI reads webhooks from a.Store() and
// connected-project names from a.ConnectedProjects().
func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Live dashboard of captured webhooks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := app.New(newConfigManager(), app.WithScriptEngine(newScriptEngine(), logScriptError))
			if err := a.Open(cmd.Context()); err != nil {
				return fmt.Errorf("tui: open store: %w", err)
			}
			defer func() { _ = a.Close() }()

			// Start the relay tunnel in the background (no-op when relay
			// URL or credentials are missing — the dashboard still shows
			// historical data from the local SQLite).
			if err := a.StartTunnel(cmd.Context()); err != nil {
				// Non-fatal: surface to stderr but keep the dashboard open.
				fmt.Fprintf(os.Stderr, "wiretap: tunnel not started: %v\n", err)
			}

			m := tui.New(a.Store(), tui.WithConnectedProjects(a.ConnectedProjects))
			p := tea.NewProgram(m, tea.WithAltScreen())
			_, err := p.Run()
			return err
		},
	}
}

// runner and its seams were deleted when the TUI switched to app.App. The
// app package owns the tunnel runner now; no per-command seam is needed.
var _ = context.Background
