package cli

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/plutack/wiretap/internal/store"
	"github.com/plutack/wiretap/internal/tui"
)

// newTUICmd builds the `wiretap tui` command. The store is obtained from the
// newPCStore seam so tests can inject an in-memory store.
func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Live dashboard of captured webhooks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st := newPCStore()
			// Start the relay tunnel in the background so the TUI gets
			// live webhooks. The tunnel reads config + credentials; if
			// either is missing it skips the tunnel and the TUI just
			// shows historical data from the local SQLite.
			go startTunnelBackground(cmd.Context())

			m := tui.New(st)
			p := tea.NewProgram(m, tea.WithAltScreen())
			_, err := p.Run()
			return err
		},
	}
}

// startTunnelBackground loads the relay config and credentials, builds a
// relayclient, and runs it. Errors are non-fatal — the TUI still shows
// historical data when the tunnel is down. Types left as a comment for the
// reader; actual implementation depends on how the local app is wired in
// the next step.
func startTunnelBackground(ctx context.Context) {
	// Load config.
	cfg, err := newConfigManager().Load()
	if err != nil {
		return
	}
	if cfg.Relay.URL == "" {
		return
	}

	// Load credentials.
	creds, err := newConfigManager().LoadCredentials()
	if err != nil {
		return
	}

	// Open the store (same as the TUI uses, but independent connection).
	db, err := store.Open(cfg.Store.Path)
	if err != nil {
		return
	}
	defer func() { _ = db.Close() }()
	if err := store.MigratePC(ctx, db); err != nil {
		return
	}
	pcStore := store.NewPCStore(db)

	// Build and run the relay client.
	c := newRelayClientRunner(relayClientConfig{
		URL:         cfg.Relay.URL,
		ClientID:    creds.ClientID,
		ClientToken: creds.ClientToken,
		Projects:    creds.Projects,
	}, pcStore)
	_ = c.Run(ctx)
}

// relayClientConfig is a small struct to pass config to the runner.
type relayClientConfig struct {
	URL         string
	ClientID    string
	ClientToken string
	Projects    []string
}

// newRelayClientRunner builds a relayclient.Client. Kept as a seam so tests
// can stub it without importing relayclient here.
//
//nolint:gochecknoglobals // test seam
var newRelayClientRunner = func(cfg relayClientConfig, st *store.PCStore) runner {
	return &noopRunner{}
}

// runner is the interface relayclient.Client satisfies (Run returning an
// error on shutdown). Declared here so cli doesn't import relayclient.
type runner interface {
	Run(ctx context.Context) error
}

// noopRunner satisfies runner; used as the zero-value default in tests
// and in production when the seam is not overridden.
type noopRunner struct{}

func (*noopRunner) Run(_ context.Context) error { return nil }

// newPCStore is the seam for building a PCStore for the TUI. Production
// opens the real on-disk database from the config; tests override it to
// return an in-memory store.
//
//nolint:gochecknoglobals // test seam
var newPCStore = func() *store.PCStore {
	cfg, err := newConfigManager().Load()
	if err != nil || cfg.Store.Path == "" {
		return nil
	}
	db, err := store.Open(cfg.Store.Path)
	if err != nil {
		return nil
	}
	_ = store.MigratePC(context.Background(), db)
	return store.NewPCStore(db)
}
