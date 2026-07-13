package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/plutack/wiretap/internal/config"
	"github.com/plutack/wiretap/internal/intercept"
	"github.com/plutack/wiretap/internal/intercept/castore"
	"github.com/plutack/wiretap/internal/intercept/shellscript"
	"github.com/plutack/wiretap/internal/store"
)

// newInterceptCmd builds the `wiretap intercept` tree: `start` (spawn an
// intercepted shell) and `stop` (revert any leftover startup-file blocks +
// remove the override-bin dir, e.g. after a crash).
func newInterceptCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "intercept",
		Short: "Intercept outbound HTTP/HTTPS traffic from a spawned shell",
	}
	cmd.AddCommand(newInterceptStartCmd(version))
	cmd.AddCommand(newInterceptStopCmd())
	return cmd
}

func newInterceptStartCmd(version string) *cobra.Command {
	var (
		noShell bool
		shell   string
	)
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the interception proxy + control API and spawn an intercepted shell",
		RunE: func(cmd *cobra.Command, _ []string) error {
			m := newConfigManager()
			cfg, err := m.Load()
			if err != nil {
				// First run with no `wiretap config init` yet: fall back to
				// defaults so interception works zero-touch.
				def := config.Default()
				cfg = &def
			}
			kind := detectShellKind(shell, cfg.Intercept.Shell)

			deps, err := resolveInterceptDeps(m, cfg, kind, version)
			if err != nil {
				return err
			}

			sess, err := interceptStart(cmd.Context(), deps)
			if err != nil {
				return err
			}
			// Stop carries the session's own background context so the cleanup
			// still runs if the caller's context is already cancelled.
			defer func() { _ = sess.Stop(context.Background()) }()

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "wiretap: interception proxy listening at http://%s\n", sess.ProxyAddr())
			fmt.Fprintf(out, "wiretap: control API at http://%s/local/{health,webhooks,captures}\n", sess.LocalAPIAddr())
			fmt.Fprintf(out, "wiretap: interception enabled for %s shell\n", kind)

			if noShell {
				fmt.Fprintln(out, "wiretap: --no-shell set; press Ctrl-C to stop.")
				waitInterrupt(cmd.Context())
				return nil
			}
			return interceptSpawn(sess, cmd.Context(), kind)
		},
	}
	cmd.Flags().BoolVar(&noShell, "no-shell", false, "start the proxy + API without spawning a shell")
	cmd.Flags().StringVar(&shell, "shell", "", "shell kind (bash|fish|powershell|gitbash); default: auto-detect from $SHELL or config")
	return cmd
}

func newInterceptStopCmd() *cobra.Command {
	var shell string
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Remove wiretap interception blocks from shell startup files",
		RunE: func(cmd *cobra.Command, _ []string) error {
			m := newConfigManager()
			cfg, err := m.Load()
			if err != nil {
				def := config.Default()
				cfg = &def
			}
			kind := detectShellKind(shell, cfg.Intercept.Shell)
			configDir, _ := m.Dir()

			files, err := interceptCleanup(kind, configDir)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(files) == 0 {
				fmt.Fprintln(out, "wiretap: no startup files found for", kind)
				return nil
			}
			fmt.Fprintf(out, "wiretap: reset interception on %d startup file(s) for %s\n", len(files), kind)
			return nil
		},
	}
	cmd.Flags().StringVar(&shell, "shell", "", "shell kind (bash|fish|powershell|gitbash); default: auto-detect")
	return cmd
}

// resolveInterceptDeps builds the intercept.Deps from the loaded config: it
// opens (and migrates) the local SQLite store so the proxy can persist
// captures and the control API can read them back, and wires the OS-specific
// CA installer. The returned deps flow unchanged into intercept.Start.
func resolveInterceptDeps(m *config.Manager, cfg *config.Config, kind shellscript.ShellKind, version string) (intercept.Deps, error) {
	configDir, err := m.Dir()
	if err != nil {
		return intercept.Deps{}, fmt.Errorf("resolve config dir: %w", err)
	}

	storePath := cfg.Store.Path
	if storePath == "" {
		storePath = filepath.Join(configDir, "wiretap.db")
	}
	db, err := store.Open(storePath)
	if err != nil {
		return intercept.Deps{}, fmt.Errorf("open store %s: %w", storePath, err)
	}
	if err := store.MigratePC(context.Background(), db); err != nil {
		_ = db.Close()
		return intercept.Deps{}, fmt.Errorf("migrate store: %w", err)
	}

	return intercept.Deps{
		ConfigDir:    configDir,
		ProxyAddr:    cfg.Intercept.ProxyAddr,
		LocalAPIAddr: cfg.Intercept.LocalAPIAddr,
		ShellKind:    kind,
		Installer:    castore.NewInstaller(configDir),
		PCStore:      store.NewPCStore(db),
		Version:      version,
	}, nil
}

// detectShellKind picks the shell kind in priority order: the --shell flag,
// then the intercept.shell config value, then $SHELL. Anything sh-compatible
// collapses to ShellBash (which generates a sh script that bash/zsh/dash/ksh
// all source).
func detectShellKind(flagValue, cfgValue string) shellscript.ShellKind {
	for _, v := range []string{flagValue, cfgValue} {
		if v == "" {
			continue
		}
		switch strings.ToLower(v) {
		case "bash", "zsh", "dash", "ksh", "sh":
			return shellscript.ShellBash
		case "gitbash":
			return shellscript.ShellGitBash
		case "fish":
			return shellscript.ShellFish
		case "powershell", "pwsh":
			return shellscript.ShellPowerShell
		}
	}
	base := strings.ToLower(filepath.Base(os.Getenv("SHELL")))
	switch {
	case strings.Contains(base, "fish"):
		return shellscript.ShellFish
	case strings.Contains(base, "powershell"), strings.Contains(base, "pwsh"):
		return shellscript.ShellPowerShell
	default:
		// sh/zsh/dash/ksh/bash and "no SHELL set" all fold to the sh script.
		return shellscript.ShellBash
	}
}

// waitInterrupt blocks until the process receives SIGINT/SIGTERM or ctx is
// done. Used by `--no-shell` so the operator can stop the proxy with Ctrl-C.
func waitInterrupt(ctx context.Context) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	select {
	case <-stop:
	case <-ctx.Done():
	}
}

// --- test seams ---------------------------------------------------------
//
//nolint:gochecknoglobals // intentional test seams, mirroring newRelayClientRunner
var (
	interceptStart = intercept.Start
	interceptSpawn = func(sess *intercept.Session, ctx context.Context, kind shellscript.ShellKind) error {
		return sess.SpawnShell(ctx, kind)
	}
	interceptCleanup = intercept.Cleanup
)
