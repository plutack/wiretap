package relayd

import (
	"context"
	"net/http"
	"sync"
)

// relayLifecycle owns accepted handlers, including upgraded WebSockets. Admission
// and completion share a mutex so draining cannot race a WaitGroup.Add.
type relayLifecycle struct {
	mu       sync.Mutex
	draining bool
	active   map[*relayWork]struct{}
	changed  chan struct{}
}
type relayWork struct {
	cancel context.CancelFunc
	tunnel bool
	force  func()
}

func (s *Server) admit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l := &s.lifecycle
		l.mu.Lock()
		if l.draining {
			l.mu.Unlock()
			w.Header().Set("Connection", "close")
			http.Error(w, "relay is draining", http.StatusServiceUnavailable)
			return
		}
		if l.active == nil {
			l.active = make(map[*relayWork]struct{})
			l.changed = make(chan struct{})
		}
		ctx, cancel := context.WithCancel(r.Context())
		work := &relayWork{cancel: cancel, tunnel: r.URL.Path == "/tunnel"}
		l.active[work] = struct{}{}
		l.mu.Unlock()
		defer func() {
			cancel()
			l.mu.Lock()
			delete(l.active, work)
			close(l.changed)
			l.changed = make(chan struct{})
			l.mu.Unlock()
		}()
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, relayWorkKey{}, work)))
	})
}

type relayWorkKey struct{}

// BeginDrain rejects new work. Existing HTTP requests can finish while tunnels
// stay alive; Shutdown subsequently cancels tunnels and joins their workers.
func (s *Server) BeginDrain() {
	s.lifecycle.mu.Lock()
	wasDraining := s.lifecycle.draining
	s.lifecycle.draining = true
	s.lifecycle.mu.Unlock()
	if !wasDraining {
		s.logger.Info("relay_draining")
	}
}

// Shutdown stops tunnels and waits for ALL accepted handlers. On expiry it
// cancels remaining work and force-closes upgraded sockets. A timeout is not a
// license to close a store still used by an uncooperative handler.
func (s *Server) Shutdown(ctx context.Context) error {
	s.BeginDrain()
	s.stopWork(false)
	if err := s.Wait(ctx); err != nil {
		s.stopWork(true)
		s.logger.Warn("relay_shutdown_forced")
		return err
	}
	s.logger.Info("relay_workers_stopped")
	return nil
}
func (s *Server) stopWork(all bool) {
	l := &s.lifecycle
	l.mu.Lock()
	defer l.mu.Unlock()
	for work := range l.active {
		if all || work.tunnel {
			work.cancel()
			if work.force != nil {
				work.force()
			}
		}
	}
}
func (s *Server) Wait(ctx context.Context) error {
	l := &s.lifecycle
	for {
		l.mu.Lock()
		if len(l.active) == 0 {
			l.mu.Unlock()
			return nil
		}
		changed := l.changed
		l.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func (s *Server) setTunnelForce(ctx context.Context, force func()) {
	if work, ok := ctx.Value(relayWorkKey{}).(*relayWork); ok {
		l := &s.lifecycle
		l.mu.Lock()
		work.force = force
		cancelled := ctx.Err() != nil
		l.mu.Unlock()
		if cancelled {
			force()
		}
	}
}
