//go:build !gui

// Stub for the default build (no `gui` tag): `wiretap gui` exists in the
// command tree so help/discovery is consistent, but actually running it errors
// with build instructions instead of pulling the Wails/CGO/webkit toolchain
// into every build. The real launcher is gui.go, compiled under -tags gui.
//
// The GUI build needs three tags (the Makefile `gui` target sets them):
//   - gui          — our gate (keeps the default build Wails-free)
//   - production   — Wails' real-app gate (without it, Wails' own stub returns
//                    "will not build without the correct build tags" at runtime)
//   - webkit2_41   — Wails' webkit API selector (4.1 on most current Linux
//                    distros; webkit2_40 on older ones). See the Makefile.

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
			"  go build -tags 'gui,production,webkit2_41' ./cmd/wiretap\n  ./wiretap gui\n\n" +
			"(use webkit2_40 instead of webkit2_41 on systems with webkit2gtk-4.0).",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(*cobra.Command, []string) error {
			return errors.New("gui: not enabled in this build; rebuild with `make gui` or `go build -tags 'gui,production,webkit2_41' ./cmd/wiretap` (see `wiretap gui --help`)")
		},
	}
}
