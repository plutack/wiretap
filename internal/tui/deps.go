package tui

import (
	"context"

	"github.com/plutack/wiretap/internal/export"
	"github.com/plutack/wiretap/internal/store"
)

// Deps is the full data surface the dashboard needs, as function fields so
// tests wire fakes over an in-memory store without constructing an app.App
// (the same "easily testable with fakes" seam the old WithConnectedProjects
// option provided, extended to every capability the GUI bindings expose).
// internal/cli/tui.go fills it in from *app.App — the TUI never imports app.
//
// The listing queries return fully-populated rows (bodies included), so the
// detail pane renders straight from the selected row; only replay/export
// refetch server-side by id inside app.App.
//
// Listings take a filter rather than a bare limit because the predicate runs in
// SQLite: a caller only ever holds a page of rows, so filtering after the fact
// could never see older traffic.
type Deps struct {
	Webhooks func(ctx context.Context, f store.WebhookFilter) (store.WebhookPage, error)
	Captures func(ctx context.Context, f store.CaptureFilter) (store.CapturePage, error)
	Sessions func(ctx context.Context, limit int) ([]store.InterceptSessionRow, error)

	Replay        func(ctx context.Context, project string, seq int64, targetURL string) (int, error)
	ExportTargets func() ([]export.Target, error)
	ExportWebhook func(ctx context.Context, project string, seq int64, target, client string) (string, error)
	ExportCapture func(ctx context.Context, id int64, target, client string) (string, error)

	Scripts          func(ctx context.Context) ([]store.ScriptRow, error)
	SetScriptEnabled func(ctx context.Context, id int64, enabled bool) error

	Status func() StatusSnapshot
}

// StatusSnapshot mirrors the GUI's StatusView (internal/gui/bindings.go) for
// the TUI header line: build version plus the resolved app state.
type StatusSnapshot struct {
	Version           string
	StoreOpen         bool
	RelayURL          string
	ForwardURL        string
	TunnelRunning     bool
	ConnectedProjects []string
}
