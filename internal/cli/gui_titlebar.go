package cli

import (
	"runtime"
	"strings"

	"github.com/plutack/wiretap/internal/app"
)

// Title-bar selection for the GUI window. Kept in an untagged file so the
// logic is unit-testable in default builds; gui.go applies it to the Wails
// window options.

// titlebarModeFrameless converts gui.native_titlebar to Wails' inverse
// Frameless option. Wails/GTK implements Frameless=true by disabling window
// decorations entirely; it does not request compositor decorations. Keep the
// safe native frame for auto/always and go frameless only after an explicit
// Linux "never" selection. Other platforms always retain their native frame.
func titlebarModeFrameless(mode string) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "never":
		return true
	default:
		return false
	}
}

// guiTitlebarMode resolves the configured title-bar preference ("auto",
// "always", "never"), tolerating a missing config file.
func guiTitlebarMode(a *app.App) string {
	if cfg, err := a.Config(); err == nil && cfg.GUI.NativeTitlebar != "" {
		return cfg.GUI.NativeTitlebar
	}
	return "auto"
}
