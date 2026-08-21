package cli

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/plutack/wiretap/internal/app"
	"github.com/plutack/wiretap/internal/tui"
)

// newTUICmd builds the `wiretap tui` command. It uses the same *app.App
// composition root as the GUI: open the local store, start the relay tunnel
// in the background, and poll the store every 500ms for the dashboard.
//
// The TUI reads the app strictly through the tui.Deps function-field seam
// (the same "thin adapter over the composition root" split the GUI's
// internal/gui bindings use), so the dashboard stays testable with fakes.
func newTUICmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Live dashboard of captured webhooks and traffic",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := app.New(newConfigManager(), app.WithScriptEngine(newScriptEngine(), logScriptError))
			if err := a.Open(cmd.Context()); err != nil {
				return fmt.Errorf("tui: open store: %w", err)
			}
			defer a.Close()

			// Start the relay tunnel in the background (no-op when relay
			// URL or credentials are missing — the dashboard still shows
			// historical data from the local SQLite).
			if err := a.StartTunnel(cmd.Context()); err != nil {
				// Non-fatal: surface to stderr but keep the dashboard open.
				fmt.Fprintf(os.Stderr, "wiretap: tunnel not started: %v\n", err)
			}

			deps := tui.Deps{
				Webhooks:          a.Webhooks,
				CapturesBySession: a.CapturesBySession,
				Sessions:          a.InterceptSessions,
				Replay:            a.ReplayWebhook,
				ExportTargets:     a.ExportTargets,
				ExportWebhook:     a.ExportWebhook,
				ExportCapture:     a.ExportCapture,
				Scripts:           a.Scripts,
				SetScriptEnabled:  a.SetScriptEnabled,
				Status: func() tui.StatusSnapshot {
					return statusSnapshot(a, version)
				},
			}

			theme := ""
			if cfg, err := a.Config(); err == nil {
				theme = cfg.TUI.Theme
			}

			m := tui.New(deps, tui.WithTheme(theme))
			p := tea.NewProgram(m, tea.WithAltScreen())
			_, err := p.Run()
			return err
		},
	}
}

// statusSnapshot mirrors the GUI bindings' Status payload for the TUI header.
func statusSnapshot(a *app.App, version string) tui.StatusSnapshot {
	v := tui.StatusSnapshot{
		Version:           version,
		StoreOpen:         a.Store() != nil,
		TunnelRunning:     a.TunnelRunning(),
		ConnectedProjects: a.ConnectedProjects(),
	}
	if cfg, err := a.Config(); err == nil {
		v.RelayURL = cfg.Relay.URL
		v.ForwardURL = cfg.Relay.ForwardURL
	}
	return v
}
