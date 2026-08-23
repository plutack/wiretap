package cli

import (
	"runtime"
	"strings"

	"github.com/plutack/wiretap/internal/app"
)

// Title-bar selection for the GUI window. Kept in an untagged file so the
// logic is unit-testable in default builds; gui.go applies it to the Wails
// window options.

// ssdDesktops are Linux sessions whose compositors draw server-side window
// decorations for frameless clients. XDG_CURRENT_DESKTOP is colon-separated
// and vendors prefix (e.g. "X-COSMIC"), so match on tokens/prefixes.
//
// Windows and macOS are deliberately absent: on Windows a frameless window
// has no bar unless the app draws one, and macOS's native NSWindow bar
// (traffic lights) is always provided by the toolkit — on both, the normal
// decorated window is already the native look. nativeTitlebarWanted never
// goes frameless off Linux regardless of the config value.
var ssdDesktops = []string{"COSMIC", "KDE"}

// nativeTitlebarWanted reports whether the window should be frameless so
// the compositor's native title bar is used. mode comes from
// gui.native_titlebar ("auto"/"always"/"never"); desktop is
// XDG_CURRENT_DESKTOP.
func nativeTitlebarWanted(mode, desktop string) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "always":
		return true
	case "never":
		return false
	}
	for _, token := range strings.Split(strings.ToUpper(desktop), ":") {
		token = strings.TrimSpace(token)
		for _, want := range ssdDesktops {
			if token == want || strings.HasPrefix(token, want+"-") || strings.HasPrefix(token, "X-"+want) {
				return true
			}
		}
	}
	return false
}

// guiTitlebarMode resolves the configured title-bar preference ("auto",
// "always", "never"), tolerating a missing config file.
func guiTitlebarMode(a *app.App) string {
	if cfg, err := a.Config(); err == nil && cfg.GUI.NativeTitlebar != "" {
		return cfg.GUI.NativeTitlebar
	}
	return "auto"
}
