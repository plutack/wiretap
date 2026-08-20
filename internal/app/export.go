package app

import (
	"context"
	"net/url"
	"strings"

	"github.com/plutack/wiretap/internal/export"
)

// This file is the app-level surface for "export as code": it converts stored
// rows (traffic captures, webhooks) into export.Request values and delegates
// snippet rendering to internal/export. CLI, TUI, and GUI all call these
// methods so every frontend produces identical snippets.

// ExportTargets returns the snippet target catalog (languages + clients).
func (a *App) ExportTargets() ([]export.Target, error) {
	return export.Targets()
}

// ExportCapture renders the *request half* of a stored traffic capture as a
// code snippet for target/client (empty client = target default).
func (a *App) ExportCapture(ctx context.Context, id int64, target, client string) (string, error) {
	c, err := a.CaptureByID(ctx, id)
	if err != nil {
		return "", err
	}
	return export.Snippet(export.Request{
		Method:  c.Method,
		URL:     c.URL,
		Headers: parseHeaders(c.ReqHeadersJSON),
		Body:    c.ReqBody,
	}, target, client)
}

// ExportWebhook renders a stored webhook as a code snippet reproducing the
// original delivery. The public URL is reconstructed from the configured
// relay tunnel endpoint (wss://host/tunnel → https://host) plus the project
// and preserved sub-path; when no relay is configured a clearly-fake
// placeholder host is used so the snippet is still copyable.
func (a *App) ExportWebhook(ctx context.Context, project string, seq int64, target, client string) (string, error) {
	w, err := a.WebhookBySeq(ctx, project, seq)
	if err != nil {
		return "", err
	}
	base := ""
	if cfg, err := a.Config(); err == nil {
		base = IngressBaseURL(cfg.Relay.URL)
	}
	if base == "" {
		base = "https://relay.invalid"
	}
	return export.Snippet(export.Request{
		Method:  w.Method,
		URL:     base + "/" + w.Project + w.Path,
		Headers: parseHeaders(w.HeadersJSON),
		Body:    w.Body,
	}, target, client)
}

// IngressBaseURL derives the relay's public HTTP ingress base from the
// configured WebSocket tunnel endpoint: scheme wss→https / ws→http and the
// well-known "/tunnel" path stripped. Returns "" when the value is empty or
// unparseable. The inverse of TunnelURLFromBase (settings.go).
func IngressBaseURL(tunnelURL string) string {
	if tunnelURL == "" {
		return ""
	}
	u, err := url.Parse(tunnelURL)
	if err != nil || u.Host == "" {
		return ""
	}
	switch u.Scheme {
	case "wss":
		u.Scheme = "https"
	case "ws":
		u.Scheme = "http"
	}
	u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/tunnel")
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimSuffix(u.String(), "/")
}
