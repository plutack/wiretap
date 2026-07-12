package relayclient

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/plutack/wiretap/internal/relayproto"
)

// This file holds the in-memory doubles the unit tests use to exercise the
// reconnect + protocol state machine without spinning up real WebSocket
// servers. The integration_test.go file covers the real coder/websocket path
// against internal/relayd via httptest.

// FakeDialer hands the next Dial call a pre-built FakeConn. Each Dial()
// returns and pops one queued session; tests queue N sessions to drive the
// reconnect loop.
type FakeDialer struct {
	mu       sync.Mutex
	sessions []*FakeConn
	dials    int
	dialErr  error
}

// Dial implements Dialer.
func (f *FakeDialer) Dial(_ context.Context, _ string, _ http.Header) (Conn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dials++
	if f.dialErr != nil && f.dials == 1 {
		// Only on the first dial; ensures the reconnect path covers a
		// failing dial followed by a working one.
		return nil, f.dialErr
	}
	if len(f.sessions) == 0 {
		return nil, errNoSessionQueued
	}
	c := f.sessions[0]
	f.sessions = f.sessions[1:]
	return c, nil
}

// Queue appends a session to be returned on the next Dial.
func (f *FakeDialer) Queue(c *FakeConn) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions = append(f.sessions, c)
}

// Dials returns the number of Dial calls observed.
func (f *FakeDialer) Dials() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dials
}

// FakeConn is an in-memory Conn implementation. It surfaces a channel of
// outbound bytes (server -> client) and a channel of inbound bytes
// (client -> server), so a test can drive the reader loop by enqueuing
// pre-encoded relayproto messages into FromServer.
type FakeConn struct {
	// FromServer is what the fake "relay" sends. Test enqueues bytes; the
	// client's reader dequeues.
	FromServer chan []byte
	// ToServer is what the client writes (ACKs, etc.). Test reads from
	// here to assert.
	ToServer chan []byte

	// closed signals ctx cancellation back to a blocked Read.
	closed chan struct{}
	once   sync.Once
}

// NewFakeConn builds a FakeConn with buffered channels so the test does not
// have to run a separate reader.
func NewFakeConn() *FakeConn {
	return &FakeConn{
		FromServer: make(chan []byte, 16),
		ToServer:   make(chan []byte, 16),
		closed:     make(chan struct{}),
	}
}

// Read implements Conn. Blocks on FromServer; returns a special error (so
// the reader loop returns and the session ends) when Close is called.
func (c *FakeConn) Read(ctx context.Context) (MessageType, []byte, error) {
	select {
	case b := <-c.FromServer:
		return MessageText, b, nil
	case <-c.closed:
		return 0, nil, errConnClosed
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

// Write implements Conn. Pushes b to ToServer; non-blocking-ish: blocks if
// the buffer is full so the test's consumer must be draining. ctx.Done is
// honoured.
func (c *FakeConn) Write(ctx context.Context, b []byte) error {
	select {
	case c.ToServer <- b:
		return nil
	case <-c.closed:
		return errConnClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close implements Conn. Idempotent; closes `closed` so menos blocked
// readers/writers wake up with errConnClosed.
func (c *FakeConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

// Send is a test helper that encodes m and enqueues it on FromServer. Used
// by tests to play server -> client messages.
func (c *FakeConn) Send(m relayproto.Message) error {
	b, err := relayproto.Encode(m)
	if err != nil {
		return err
	}
	c.FromServer <- b
	return nil
}

// SendAfter enqueues m after the delay. Helps tests reproduce a server
// "thinking" before pushing the next webhook.
func (c *FakeConn) SendAfter(m relayproto.Message, d time.Duration) {
	go func() {
		<-time.After(d)
		_ = c.Send(m)
	}()
}

// errNoSessionQueued is returned by a FakeDialer with no sessions left.
var errNoSessionQueued = errSentinel("no session queued")

// errConnClosed is returned by Read/Write on a closed FakeConn.
var errConnClosed = errSentinel("connection closed")

// errSentinel is a tiny private error type so we can use errors.Is without
// exporting sentinels that only matter to this test file.
type errSentinel string

func (e errSentinel) Error() string { return string(e) }
func (e errSentinel) Is(target error) bool {
	s, ok := target.(errSentinel)
	return ok && s == e
}
