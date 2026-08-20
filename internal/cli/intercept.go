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
	cmd.AddCommand(newInterceptTrustCACmd())
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
			if err := writePIDFile(deps.ConfigDir, os.Getpid()); err != nil {
				_ = sess.Stop(context.Background())
				return fmt.Errorf("write pid file: %w", err)
			}
			defer func() {
				removePIDFile(deps.ConfigDir)
				_ = sess.Stop(context.Background())
			}()

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "wiretap: interception proxy listening at http://%s\n", sess.ProxyAddr())
			fmt.Fprintf(out, "wiretap: control API at http://%s/local/{health,webhooks,captures}\n", sess.LocalAPIAddr())
			fmt.Fprintf(out, "wiretap: interception enabled for %s shell\n", kind)

			if noShell {
				fmt.Fprintln(out, "wiretap: --no-shell set; press Ctrl-C to stop.")
				waitInterrupt(cmd.Context())
				return nil
			}
			// Shell mode: without a handler, SIGTERM (what `wiretap intercept
			// stop` sends) would kill this process outright — the deferred
			// cleanup would never run and the spawned shell would be orphaned
			// with proxy env pointing at a dead listener. NotifyContext cancels
			// the exec.CommandContext inside SpawnShell, which tears down the
			// child shell, lets Run return, and unwinds the defers above.
			sctx, cancelSignals := signal.NotifyContext(cmd.Context(), syscall.SIGTERM)
			defer cancelSignals()
			err = interceptSpawn(sess, sctx, kind)
			if sctx.Err() != nil {
				// Signal-triggered shutdown is a clean exit, not an error.
				fmt.Fprintln(out, "wiretap: interception session stopped")
				return nil
			}
			return err
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
			out := cmd.OutOrStdout()

			if pid, err := readPIDFile(configDir); err == nil {
				if processAlive(pid) {
					if err := terminateProcess(pid); err != nil {
						fmt.Fprintf(out, "wiretap: signal pid %d: %v\n", pid, err)
					} else {
						fmt.Fprintf(out, "wiretap: stopping intercept session (pid %d)\n", pid)
					}
					if !waitProcessExit(pid) {
						fmt.Fprintf(out, "wiretap: pid %d did not exit within %s; listeners may still be open\n", pid, pidStopTimeout)
					}
				}
				removePIDFile(configDir)
			}

			files, err := interceptCleanup(kind, configDir)
			if err != nil {
				return err
			}
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
		ConfigDir:     configDir,
		ProxyAddr:     cfg.Intercept.ProxyAddr,
		LocalAPIAddr:  cfg.Intercept.LocalAPIAddr,
		ShellKind:     kind,
		Installer:     castore.NewInstaller(configDir),
		PCStore:       store.NewPCStore(db),
		Version:       version,
		ScriptEngine:  newScriptEngine(),
		OnScriptError: logScriptError,
	}, nil
}

// newInterceptTrustCACmd installs the wiretap CA into the system trust store.
// This is the one command that needs root (on Linux it writes to
// /usr/local/share/ca-certificates/ and runs update-ca-certificates). It's
// optional: `wiretap intercept start` works without it because the shims pin
// the CA cert directly (--cacert, http.sslCAInfo, NODE_EXTRA_CA_CERTS,
// SSL_CERT_FILE). TrustSystem is only needed for tools that read the system
// trust store exclusively (e.g. a system Python without SSL_CERT_FILE set).
//
// Run once: `sudo wiretap intercept trust-ca`
func newInterceptTrustCACmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trust-ca",
		Short: "Install the wiretap CA into the system trust store (needs root, optional)",
		Long: "Install the wiretap interception CA into the OS trust store so tools that\n" +
			"only read the system store (not env vars or --cacert flags) trust it.\n\n" +
			"This is OPTIONAL. The three supported tools (curl, git, node) already trust\n" +
			"the CA via shims and env vars. You only need this if you want other tools\n" +
			"(e.g. system Python) to trust the CA too.\n\n" +
			"Needs root on Linux: sudo wiretap intercept trust-ca",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m := newConfigManager()
			configDir, err := m.Dir()
			if err != nil {
				return fmt.Errorf("resolve config dir: %w", err)
			}
			installer := castore.NewInstaller(configDir)
			ctx := cmd.Context()
			// Ensure the CA exists first (generates + persists, no root needed).
			if _, err := installer.EnsureCA(ctx); err != nil {
				return fmt.Errorf("ensure CA: %w", err)
			}
			// Now install into the system trust store (needs root).
			if err := installer.TrustSystem(ctx); err != nil {
				return fmt.Errorf("trust-ca: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "wiretap: CA installed into system trust store")
			return nil
		},
	}
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
