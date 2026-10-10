package logging

import (
	"context"
	"io"
	"log/slog"
)

type contextKey struct{}

var silent = slog.New(slog.NewTextHandler(io.Discard, nil))

// WithContext carries an instance-owned logger without touching global logging.
func WithContext(ctx context.Context, logger *slog.Logger) context.Context {
	if logger == nil {
		logger = silent
	}
	return context.WithValue(ctx, contextKey{}, logger)
}

// FromContext returns a silent logger when callers have not configured logging.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(contextKey{}).(*slog.Logger); ok {
		return logger
	}
	return silent
}
