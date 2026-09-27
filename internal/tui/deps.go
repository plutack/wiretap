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
// The listing queries return list projections rather than full rows: the lists
// are re-queried on a short interval, and reading retained payloads every time
// is what made the dashboard slow. TheDetail queries load one full row when it
// is opened, which is where the bodies are needed.
type Deps struct {
	Webhooks      func(ctx context.Context, f store.WebhookFilter) (store.WebhookSummaryPage, error)
	Captures      func(ctx context.Context, f store.CaptureFilter) (store.CaptureSummaryPage, error)
	WebhookDetail func(ctx context.Context, project string, seq int64) (*store.WebhookRow, error)
	CaptureDetail func(ctx context.Context, id int64) (*store.TrafficCaptureRow, error)
	Sessions      func(ctx context.Context, limit int) ([]store.InterceptSessionRow, error)

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
