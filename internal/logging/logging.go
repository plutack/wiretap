// Package logging configures Wiretap's shared structured logging foundation.
// Callers must not attach payloads, raw URLs, headers, or peer-supplied errors.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// New builds an isolated logger without changing the process-wide default.
// Empty format and level select text and info respectively. The caller owns w.
func New(w io.Writer, format, level string) (*slog.Logger, error) {
	var severity slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		severity = slog.LevelInfo
	case "debug":
		severity = slog.LevelDebug
	case "warn":
		severity = slog.LevelWarn
	case "error":
		severity = slog.LevelError
	default:
		return nil, fmt.Errorf("invalid log level: expected debug, info, warn, or error")
	}
	if w == nil {
		return nil, fmt.Errorf("log output is required")
	}
	opts := &slog.HandlerOptions{Level: severity, ReplaceAttr: redact}
	var handler slog.Handler
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text":
		handler = slog.NewTextHandler(w, opts)
	case "json":
		handler = slog.NewJSONHandler(w, opts)
	default:
		return nil, fmt.Errorf("invalid log format: expected text or json")
	}
	return slog.New(handler), nil
}

// Redact common sensitive attribute names as defense in depth, including nested
// groups. This is not a substitute for choosing safe fields at call sites.
func redact(_ []string, attr slog.Attr) slog.Attr {
	key := strings.ReplaceAll(strings.ToLower(attr.Key), "-", "_")
	switch key {
	case "authorization", "proxy_authorization", "cookie", "set_cookie", "password", "secret", "token", "admin_token", "client_token", "body", "request_body", "response_body", "headers", "raw_headers", "url", "query":
		attr.Value = slog.StringValue("[REDACTED]")
	}
	return attr
}
