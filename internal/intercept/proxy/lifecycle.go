package proxy

import (
	"context"
	"errors"
	"net"
	"time"
)

type proxyWork struct{ cancel context.CancelFunc }

func (p *Proxy) begin(ctx context.Context) (context.Context, func(), bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.draining {
		return nil, nil, false
	}
	if p.work == nil {
		p.work = make(map[*proxyWork]struct{})
		p.changed = make(chan struct{})
	}
	ctx, cancel := context.WithCancel(ctx)
	op := &proxyWork{cancel: cancel}
	p.work[op] = struct{}{}
	return ctx, func() {
		cancel()
		p.mu.Lock()
		delete(p.work, op)
		close(p.changed)
		p.changed = make(chan struct{})
		p.mu.Unlock()
	}, true
}

// Stop drains accepted exchanges, including CONNECT handlers. On deadline it
// cancels upstream I/O and closes tracked client sockets to interrupt TLS/read.
func (p *Proxy) Stop(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	p.mu.Lock()
	p.draining = true
	srv, ln := p.server, p.ln
	p.mu.Unlock()
	if srv == nil {
		return nil
	}
	err := srv.Shutdown(ctx)
	if err != nil {
		p.force()
		_ = srv.Close()
	}
	for {
		p.mu.Lock()
		if len(p.work) == 0 {
			p.mu.Unlock()
			break
		}
		changed := p.changed
		p.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			p.force()
			return errors.Join(err, ctx.Err())
		}
	}
	if ln != nil {
		if closeErr := ln.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			err = errors.Join(err, closeErr)
		}
	}
	return err
}
func (p *Proxy) force() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for op := range p.work {
		op.cancel()
	}
	for conn := range p.conns {
		_ = conn.Close()
	}
}

// Wait can be used after force to verify all capture/store users have exited.
func (p *Proxy) Wait(ctx context.Context) error {
	for {
		p.mu.Lock()
		if len(p.work) == 0 {
			p.mu.Unlock()
			return nil
		}
		changed := p.changed
		p.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func (p *Proxy) isDraining() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.draining }
