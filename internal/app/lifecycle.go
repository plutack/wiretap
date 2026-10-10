package app

import (
	"context"
	"errors"
	"time"
)

const ShutdownTimeout = 5 * time.Second

var ErrClosing = errors.New("app: shutdown in progress")

// beginOperation prevents admission once closing starts and tracks cancellable
// replay/forward operations so their store stays open until they leave.
func (a *App) beginOperation(ctx context.Context) (context.Context, func(), error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if parent, ok := ctx.Value(appOperationKey{}).(*appOperation); ok {
		if _, active := a.operations[parent]; active {
			return ctx, func() {}, nil
		}
	}
	if a.closing {
		return nil, nil, ErrClosing
	}
	if a.operations == nil {
		a.operations = make(map[*appOperation]struct{})
		a.operationsChanged = make(chan struct{})
	}
	ctx, cancel := context.WithCancel(ctx)
	op := &appOperation{cancel: cancel}
	a.operations[op] = struct{}{}
	ctx = context.WithValue(ctx, appOperationKey{}, op)
	return ctx, func() {
		cancel()
		a.mu.Lock()
		delete(a.operations, op)
		close(a.operationsChanged)
		a.operationsChanged = make(chan struct{})
		a.mu.Unlock()
	}, nil
}

type appOperationKey struct{}
type appOperation struct{ cancel context.CancelFunc }

// Shutdown is bounded. If an injected worker ignores cancellation, return a
// timeout with the store still open; a subsequent Shutdown may finish cleanup.
func (a *App) Shutdown(ctx context.Context) error {
	a.mu.Lock()
	a.closing = true
	a.mu.Unlock()
	a.logger.Info("client_shutdown_requested")
	if err := a.StopTunnelContext(ctx); err != nil {
		a.cancelOperations()
		return err
	}
	for {
		a.mu.Lock()
		if len(a.operations) == 0 {
			a.mu.Unlock()
			break
		}
		changed := a.operationsChanged
		a.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			a.cancelOperations()
			a.logger.Warn("client_shutdown_timed_out")
			return ctx.Err()
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.db == nil {
		return nil
	}
	if err := a.db.Close(); err != nil {
		a.logger.Error("client_store_close_failed")
		return err
	}
	a.db = nil
	a.store = nil
	a.logger.Info("client_store_closed")
	a.logger.Info("client_shutdown_completed")
	return nil
}

func (a *App) cancelOperations() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for op := range a.operations {
		op.cancel()
	}
}

func (a *App) StopTunnelContext(ctx context.Context) error {
	a.mu.Lock()
	cancel, done := a.tunnelCancel, a.tunnelDone
	a.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
	case <-ctx.Done():
		a.logger.Warn("client_tunnel_stop_timed_out")
		return ctx.Err()
	}
	a.mu.Lock()
	if a.tunnelDone == done {
		a.tunnelCancel = nil
		a.tunnelDone = nil
		a.tunnelCtx = nil
		a.connectedProjects = nil
	}
	a.mu.Unlock()
	a.logger.Info("client_tunnel_stopped")
	return nil
}
