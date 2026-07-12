// Package cli contains the cobra command tree for the wiretap binary. The
// tree is built fresh by NewRootCmd so there is no package-level command
// state — each invocation and each test gets its own isolated tree.
//
// Subcommands that need config or relay HTTP access take their dependencies
// through options, which keeps them testable with httptest.NewServer and
// temp config dirs instead of the real network or filesystem.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/plutack/wiretap/internal/config"
)

// Execute runs the root command for the wiretap binary. It is the only
// entry point called from cmd/wiretap/main.go.
func Execute(version string) error {
	return NewRootCmd(version).Execute()
}

// NewRootCmd builds the command tree. version is surfaced via `wiretap
// version` and the `--version` flag.
func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "wiretap",
		Short:         "Capture HTTP traffic and inbound webhooks",
		Long:          "wiretap intercepts outbound traffic locally and receives inbound webhooks over a self-hosted relay.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true, // handlers return errors; main.go prints them
	}
	root.AddCommand(newVersionCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newRelayCmd())
	return root
}

// newVersionCmd prints the embedded build version.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the wiretap version",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), cmd.Root().Version)
		},
	}
}

// newConfigCmd groups `wiretap config` subcommands. Today only `init`.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configuration management",
	}
	cmd.AddCommand(newConfigInitCmd())
	return cmd
}

// newConfigInitCmd writes Default() to disk.
//
// The config manager is obtained from the newConfigManager factory variable
// rather than by calling config.NewManager() directly. This is the only
// piece of package-level mutable state in cli and exists solely as a test
// seam: cli_test.go swaps it for a temp-dir-backed manager and restores it
// via t.Cleanup. (The "no package-level mutable state" rule in PLAN.md
// targets runtime config like clocks/ids that affect logic; a documented
// test-hook var is the idiomatic Go exception.)
func newConfigInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a default config file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			force, _ := cmd.Flags().GetBool("force")
			m := newConfigManager()
			path, err := m.Init(force)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", path)
			return nil
		},
	}
	cmd.Flags().BoolP("force", "f", false, "overwrite an existing config file")
	return cmd
}

// newConfigManager is the seam used by newConfigInitCmd. Production returns
// a real manager backed by the OS config dir. Tests override it (capturing
// the original first, restoring via t.Cleanup) to point at t.TempDir().
//
//nolint:gochecknoglobals // intentional test seam; see comment in newConfigInitCmd
var newConfigManager = func() *config.Manager {
	return config.NewManager()
}
