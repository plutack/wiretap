package gui

import (
	"context"
	"fmt"

	"github.com/plutack/wiretap/internal/export"
)

// Export bindings: "export as code" for the detail panes. All heavy lifting
// lives in internal/app + internal/export (the embedded httpsnippet engine);
// these methods only marshal.

// TargetView is one snippet language in the export dropdown, with its
// available clients. Mirrors export.Target.
type TargetView struct {
	Key           string       `json:"key"`
	Title         string       `json:"title"`
	DefaultClient string       `json:"default"`
	Clients       []ClientView `json:"clients"`
}

// ClientView is one concrete library/tool within a TargetView.
type ClientView struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// ExportTargets returns the snippet target catalog for the export dropdowns.
func (b *Bindings) ExportTargets() ([]TargetView, error) {
	ts, err := b.app.ExportTargets()
	if err != nil {
		return nil, fmt.Errorf("export targets: %w", err)
	}
	return targetViews(ts), nil
}

// ExportCapture renders the request half of a traffic capture as a code
// snippet. Empty client selects the target's default.
func (b *Bindings) ExportCapture(id int64, target, client string) (string, error) {
	out, err := b.app.ExportCapture(context.Background(), id, target, client)
	if err != nil {
		return "", fmt.Errorf("export capture %d: %w", id, err)
	}
	return out, nil
}

// ExportWebhook renders a stored webhook delivery as a code snippet against
// the relay's public ingress URL.
func (b *Bindings) ExportWebhook(project string, seq int64, target, client string) (string, error) {
	out, err := b.app.ExportWebhook(context.Background(), project, seq, target, client)
	if err != nil {
		return "", fmt.Errorf("export webhook %s/%d: %w", project, seq, err)
	}
	return out, nil
}

func targetViews(ts []export.Target) []TargetView {
	out := make([]TargetView, 0, len(ts))
	for _, t := range ts {
		clients := make([]ClientView, 0, len(t.Clients))
		for _, c := range t.Clients {
			clients = append(clients, ClientView{Key: c.Key, Title: c.Title})
		}
		out = append(out, TargetView{
			Key:           t.Key,
			Title:         t.Title,
			DefaultClient: t.DefaultClient,
			Clients:       clients,
		})
	}
	return out
}
