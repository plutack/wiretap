//go:build gui

// This file is compiled only with the `gui` build tag. It wires the Wails v3
// runtime to the already-tested composition root (internal/app) via the
// binding layer (internal/gui) and launches the desktop window as the
// `wiretap gui` command.
//
// A working GUI build needs the `gui` tag plus a webkit selection tag (the
// Makefile `gui` target sets them):
//
//   go build -tags 'gui,gtk3' ./cmd/wiretap
//   ./wiretap gui
//
//   - gui   — our gate (keeps the default build Wails/CGO/webkit-free)
//   - gtk3  — Wails v3's Linux webkit selector: gtk3 targets
//             webkit2gtk-4.1 (GTK3, present on most current distros);
//             the default targets webkitgtk-6.0 (GTK4)
//   - production — Wails' release gate; add it for release-style builds
//             (the Makefile `gui` target does). Without it the app runs in
//             dev mode with devtools enabled.
//
// Everything testable lives in internal/gui (no Wails import, no CGO/webkit
// dependency); this file is intentionally thin launch glue.

package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/wailsapp/wails/v3/pkg/application"

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

	wailsApp := application.New(application.Options{
		Name: "wiretap",
		Icon: guiassets.Icon,
		Linux: application.LinuxOptions{
			ProgramName: "wiretap",
		},
		Services: []application.Service{
			application.NewService(bindings),
		},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(guiassets.Assets),
		},
		OnShutdown: func() {
			_ = a.Close()
		},
	})

	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		URL:   "/",
		Title: "wiretap",
		// Match the UI's --wt-bg (#080a0c) so the window never flashes white
		// while the webview boots — the closest thing to "native dark mode"
		// Wails offers; form-control popups follow the CSS color-scheme:dark.
		StartState:       application.WindowStateMaximised,
		BackgroundColour: application.NewRGB(8, 10, 12),
		Width:            1024,
		Height:           720,
		MinWidth:         720,
		MinHeight:        480,
		Linux: application.LinuxWindow{
			WebviewGpuPolicy: application.WebviewGpuPolicyOnDemand,
		},
	})

	if err := wailsApp.Run(); err != nil {
		return fmt.Errorf("gui: %w", err)
	}
	return nil
}
