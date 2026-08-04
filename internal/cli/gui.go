//go:build gui

// This file is compiled only with the `gui` build tag. It wires the Wails v2
// runtime to the already-tested composition root (internal/app) via the binding
// layer (internal/gui) and launches the desktop window as the `wiretap gui`
// command.
//
// A working GUI build needs three tags (the Makefile `gui` target sets them):
//
//   go build -tags 'gui,production,webkit2_41' ./cmd/wiretap
//   ./wiretap gui
//
//   - gui         — our gate (keeps the default build Wails/CGO/webkit-free)
//   - production   — Wails' real-app gate; without it Wails' own stub returns
//                    "will not build without the correct build tags" at runtime
//   - webkit2_41   — Wails' webkit API selector (4.1 on most current Linux
//                    distros; webkit2_40 on older ones).
//
// Everything testable lives in internal/gui (no Wails import, no CGO/webkit
// dependency); this file is intentionally thin launch glue.

package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/linux"

	guiassets "github.com/plutack/wiretap"
	"github.com/plutack/wiretap/internal/app"
	"github.com/plutack/wiretap/internal/config"
	"github.com/plutack/wiretap/internal/gui"
)

// newGUICmd builds the `wiretap gui` subcommand that opens a Wails dashboard.
// It is registered in NewRootCmd (root.go) regardless of the gui build tag; the
// !gui build supplies a stub in gui_stub.go that explains how to enable it.
func newGUICmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gui",
		Short: "Open the wiretap dashboard (Wails GUI)",
		Long: "Launch the desktop dashboard with two tabs (Webhooks, Traffic) and a " +
			"replay button. This command requires a GUI-enabled build.\n\n" +
			"  make gui\n  ./wiretap gui\n\n" +
			"See `wiretap gui --help` in a non-GUI build for the manual go build form.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runGUI(cmd.Context(), version)
		},
	}
	return cmd
}

// runGUI is the launch glue, kept out of the cobra closure so it reads cleanly.
// It owns the *app.App lifecycle for the duration of the window.
func runGUI(parent context.Context, version string) error {
	mgr := config.NewManager()
	a := app.New(mgr, app.WithScriptEngine(newScriptEngine(), logScriptError))

	// Open the local store before serving any view; the GUI is read-only w.r.t.
	// the store except for replay, which targets an external URL.
	if err := a.Open(parent); err != nil {
		return fmt.Errorf("gui: open store: %w", err)
	}
	defer a.Close()

	// Connect the relay tunnel in the background (no-op when relay URL or
	// credentials are missing — the dashboard still shows historical data).
	if err := a.StartTunnel(parent); err != nil {
		// Non-fatal: surface to stderr but keep the window open.
		fmt.Fprintf(os.Stderr, "wiretap: tunnel not started: %v\n", err)
	}

	bindings := gui.New(a, gui.WithVersion(version))

	err := wails.Run(&options.App{
		Title: "wiretap",
		// Wails finds ui/index.html inside the embed.FS and strips the "ui/"
		// prefix automatically (see guiassets.go).
		Assets:    guiassets.Assets,
		Width:     1024,
		Height:    720,
		MinWidth:  720,
		MinHeight: 480,
		Bind:      []any{bindings},
		OnShutdown: func(ctx context.Context) {
			_ = a.Close()
		},
		Linux: &linux.Options{
			WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand,
		},
	})
	if err != nil {
		return fmt.Errorf("gui: %w", err)
	}
	return nil
}
