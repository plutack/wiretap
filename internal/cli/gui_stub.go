//go:build !gui

// Stub for the default build (no `gui` tag): `wiretap gui` exists in the
// command tree so help/discovery is consistent, but actually running it errors
// with build instructions instead of pulling the Wails/CGO/webkit toolchain
// into every build. The real launcher is gui.go, compiled under -tags gui.
//
// The GUI build needs the `gui` tag plus a webkit selection tag (the Makefile
// `gui` target sets them):
//   - gui   — our gate (keeps the default build Wails-free)
//   - gtk3  — Wails v3's Linux webkit selector (webkit2gtk-4.1 / GTK3, present
//             on most current distros). The default targets webkitgtk-6.0
//             (GTK4) instead. Add `production` for release-style builds.

package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

// newGUICmd returns a `wiretap gui` command that explains the GUI must be
// enabled at build time. See gui.go (build-tagged `gui`) for the launcher.
func newGUICmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "gui",
		Short: "Open the wiretap dashboard (Wails GUI)",
		Long: "Launch the desktop dashboard. The GUI is opt-in: rebuild the " +
			"wiretap binary with the GUI build tags to enable it.\n\n" +
			"  make gui\n  ./wiretap gui\n\n" +
			"or, without make:\n" +
			"  go build -tags 'gui,gtk3' ./cmd/wiretap\n  ./wiretap gui\n\n" +
			"(omit gtk3 on systems that provide webkitgtk-6.0 / GTK4).",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(*cobra.Command, []string) error {
			return errors.New("gui: not enabled in this build; rebuild with `make gui` or `go build -tags 'gui,gtk3' ./cmd/wiretap` (see `wiretap gui --help`)")
		},
	}
}
