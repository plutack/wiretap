package intercept

import (
	"context"
	"net/http"
	"sync"
)

// apiLifecycle tracks handlers as http.Server.Close does not join them.
type apiLifecycle struct {
	mu       sync.Mutex
	draining bool
	active   map[*apiWork]struct{}
	changed  chan struct{}
}
type apiWork struct{ cancel context.CancelFunc }

func (l *apiLifecycle) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		if l.draining {
			l.mu.Unlock()
			http.Error(w, "control API is draining", 503)
			return
		}
		if l.active == nil {
			l.active = make(map[*apiWork]struct{})
			l.changed = make(chan struct{})
		}
		ctx, cancel := context.WithCancel(r.Context())
		work := &apiWork{cancel}
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
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (l *apiLifecycle) drain(force bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.draining = true
	if force {
		for work := range l.active {
			work.cancel()
		}
	}
}
func (l *apiLifecycle) wait(ctx context.Context) error {
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
