package relayclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/plutack/wiretap/internal/relayproto"
	"github.com/plutack/wiretap/internal/store"
	"github.com/plutack/wiretap/internal/testutil"
)

// Config carries the static parameters a Client uses for every dial. They
// do not change between reconnects; loaded from wiretap's config.yaml.
type Config struct {
	// URL is the WebSocket endpoint of the relay, e.g.
	// "wss://relay.example.com/tunnel". The client upgrades HTTP to WebSocket
	// via coder/websocket; scheme must be ws:// or wss://.
	URL string
	// ClientID and ClientToken are the credentials returned by POST /register
	// or loaded from the credentials file. Sent as HTTP basic auth on every
	// dial; the relay validates them against the clients table.
	ClientID    string
	ClientToken string
	// Projects is the set of project paths this PC owns. The client sends
	// last_seqs for each in HELLO. The list should match what the relay has
	// bound to this client (mismatches cause the relay to push unknown
	// projects, which the client ignores with a warning).
	Projects []string
}

// Callbacks lets callers subscribe to lifecycle events without coupling the
// client to a specific consumer (TUI, GUI, log-only). All callbacks are
// invoked from the client's run goroutine; they must not block. Nil fields
// are no-ops.
type Callbacks struct {
	// OnWebhook fires after a PUSH is persisted to PCStore and the ACK has
	// been queued. Receives the row as stored, including RawHeaders / Body.
	OnWebhook func(row store.WebhookRow)
	// OnConnect fires once per successful dial after OK is received.
	OnConnect func(projects []string)
	// OnDisconnect fires when a session ends (error or ctx cancel) and the
	// reconnect loop is about to back off. Argument is the error that ended
	// the session; nil when the outer ctx was cancelled.
	OnDisconnect func(err error)
}

// Client dials and runs the tunnel protocol against a wiretap-relay. Call
// Run once with a long-lived context; cancel it to shut down. Reconnects
// with exponential backoff (1s -> 30s, +/=50 percent jitter) until ctx is
// cancelled.
type Client struct {
	cfg       Config
	store     *store.PCStore
	clock     testutil.Clock
	dialer    Dialer
	backoff   Backoff
	callbacks Callbacks
}

// Option configures a Client. Passed to New.
type Option func(*Client)

// WithClock injects a clock; tests pass a FakeClock so received_at / stored_at
// are deterministic. Defaults to SystemClock.
func WithClock(c testutil.Clock) Option { return func(c2 *Client) { c2.clock = c } }

// WithDialer injects the WebSocket dialer; tests pass a FakeDialer to keep
// the protocol loop entirely in-memory. Defaults to WSNetDialer, which wraps
// github.com/coder/websocket.Dial.
func WithDialer(d Dialer) Option { return func(c2 *Client) { c2.dialer = d } }

// WithBackoff injects a backoff strategy; tests pass a FakeBackoff that
// returns predictable delays (or zero, to keep tests fast). Defaults to
// ExponentialBackoff with 1s base, 30s cap, and 50 percent jitter.
func WithBackoff(b Backoff) Option { return func(c2 *Client) { c2.backoff = b } }

// WithCallbacks subscribes to lifecycle events. See Callbacks docs.
func WithCallbacks(cb Callbacks) Option { return func(c2 *Client) { c2.callbacks = cb } }

// New builds a Client. store is required; it is consulted on every dial to
// load the per-project cursor sent in HELLO and used to persist incoming
// webhooks.
func New(cfg Config, st *store.PCStore, opts ...Option) *Client {
	c := &Client{
		cfg:     cfg,
		store:   st,
		clock:   testutil.SystemClock{},
		dialer:  WSNetDialer{},
		backoff: &ExponentialBackoff{Base: time.Second, Cap: 30 * time.Second, Jitter: 0.5, Rand: rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0))},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// ErrShutdown is returned by Run when its context is cancelled. It signals
// the reconnect loop to terminate rather than retry; callers can use
// errors.Is to distinguish "stopped by user" from a fatal dial error.
var ErrShutdown = errors.New("relayclient: shutdown requested")

// Run dials the relay and runs the tunnel protocol forever, reconnecting on
// any failure. Returns ErrShutdown when ctx is cancelled, or the first
// non-recoverable error (currently none — every session error is retried).
//
// Cancelling ctx aborts any in-flight dial and any pending read; the session
// goroutine exits and Run returns ErrShutdown.
func (c *Client) Run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return ErrShutdown
		}
		err := c.runOnce(ctx)
		if c.callbacks.OnDisconnect != nil {
			c.callbacks.OnDisconnect(err)
		}
		if ctx.Err() != nil {
			return ErrShutdown
		}
		// Recoverable: back off and redial.
		wait := c.backoff.Next()
		select {
		case <-ctx.Done():
			return ErrShutdown
		case <-time.After(wait):
		}
	}
}

// runOnce dials, runs the protocol loop until the session ends, and returns
// the cause. Never retries — Run handles that.
func (c *Client) runOnce(ctx context.Context) error {
	lastSeqs, err := c.loadCursors(ctx)
	if err != nil {
		return fmt.Errorf("load cursors: %w", err)
	}
	conn, err := c.dialer.Dial(ctx, c.cfg.URL, c.authHeaders())
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	// Send HELLO.
	hello := relayproto.Hello{
		Base:        relayproto.Base{Type: relayproto.TypeHello},
		ClientID:    c.cfg.ClientID,
		ClientToken: c.cfg.ClientToken,
		LastSeqs:    lastSeqs,
	}
	if err := writeMsg(ctx, conn, hello); err != nil {
		return fmt.Errorf("write hello: %w", err)
	}

	// Read OK.
	msg, err := readMsg(ctx, conn)
	if err != nil {
		return fmt.Errorf("read ok: %w", err)
	}
	ok, isOK := msg.(relayproto.OK)
	if !isOK {
		return fmt.Errorf("expected ok, got %T", msg)
	}
	if c.callbacks.OnConnect != nil {
		c.callbacks.OnConnect(ok.Projects)
	}

	// Session loop: read PUSH, store, ack. writer goroutine for sends
	// (current sends are just ACKs returned synchronously after a successful
	// store, so a single goroutine suffices; the writer seam exists in case
	// we add REPLAY from the local app later).
	sessCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	ackCh := make(chan relayproto.Ack, 16)
	go c.writer(sessCtx, conn, ackCh)
	return c.reader(sessCtx, conn, ackCh)
}

// loadCursors reads the highest persisted seq per project from the local
// PCStore. These become last_seqs in HELLO; the relay resumes pushing from
// this point. A missing project yields 0 (push from start).
func (c *Client) loadCursors(ctx context.Context) (map[string]int64, error) {
	out := make(map[string]int64, len(c.cfg.Projects))
	for _, p := range c.cfg.Projects {
		seq, err := c.store.LastSeq(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("last_seq %q: %w", p, err)
		}
		out[p] = seq
	}
	return out, nil
}

// authHeaders returns the HTTP headers attached to the dial request. We use
// basic auth because that is what the relay's HandleTunnel expects; the
// Authorization header survives the WebSocket handshake.
func (c *Client) authHeaders() http.Header {
	h := http.Header{}
	h.Set("Authorization", basicAuth(c.cfg.ClientID, c.cfg.ClientToken))
	return h
}

// reader is the main protocol loop. It reads messages, validates direction,
// and dispatches PUSH to StoreWebhook + queues an ACK. Other relay->PC
// messages are currently unexpected; we log them and continue.
func (c *Client) reader(ctx context.Context, conn Conn, ackCh chan<- relayproto.Ack) error {
	for {
		msg, err := readMsg(ctx, conn)
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		if err := relayproto.ValidateDirection(msg, relayproto.DirRelayToPC); err != nil {
			// Server sent a PC->relay message; protocol violation, but we
			// tolerate it to keep the session alive.
			continue
		}
		push, ok := msg.(relayproto.Push)
		if !ok {
			// E.g. relayproto.Error from the server; log and keep going.
			continue
		}
		row := pushToRow(push)
		inserted, err := c.store.StoreWebhook(ctx, row, c.clock.Now())
		if err != nil {
			// Storage error is the only thing that ends the session.
			return fmt.Errorf("store webhook %s/%d: %w", push.Project, push.Seq, err)
		}
		// Always ack, even if duplicated: the relay uses the ack to advance
		// acked_seq; an idempotent store + redundant ack is the safe pair.
		ack := relayproto.Ack{
			Base:    relayproto.Base{Type: relayproto.TypeAck},
			Project: push.Project,
			UpToSeq: push.Seq,
		}
		select {
		case ackCh <- ack:
		case <-ctx.Done():
			return ctx.Err()
		}
		if c.callbacks.OnWebhook != nil && inserted {
			c.callbacks.OnWebhook(row)
		}
	}
}

// writer drains ackCh and writes ACKs to the wire. Cancelling ctx lets the
// pending send drop; the deferred conn.Close handles teardown.
func (c *Client) writer(ctx context.Context, conn Conn, ackCh <-chan relayproto.Ack) {
	for {
		select {
		case <-ctx.Done():
			return
		case ack, ok := <-ackCh:
			if !ok {
				return
			}
			if err := writeMsg(ctx, conn, ack); err != nil {
				return
			}
		}
	}
}

// pushToRow converts a wire Push to a stored WebhookRow. Headers are
// re-serialised as JSON for the queryable column; RawHeaders / Body travel
// byte-exact. This mirrors relayd.rowToPush in reverse.
func pushToRow(p relayproto.Push) store.WebhookRow {
	headersJSON, _ := json.Marshal(p.Headers)
	return store.WebhookRow{
		Project:     p.Project,
		Seq:         p.Seq,
		ReceivedAt:  time.Unix(p.ReceivedAt, 0).UTC(),
		SourceIP:    p.SourceIP,
		Method:      p.Method,
		Path:        p.Path,
		HeadersJSON: string(headersJSON),
		RawHeaders:  p.RawHeaders,
		Body:        p.Body,
	}
}

// readMsg / writeMsg are thin wrappers over relayproto + the Conn interface
// so tests substitute a FakeConn without touching real WebSocket plumbing.
func readMsg(ctx context.Context, conn Conn) (relayproto.Message, error) {
	_, b, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	return relayproto.Decode(b)
}

func writeMsg(ctx context.Context, conn Conn, m relayproto.Message) error {
	b, err := relayproto.Encode(m)
	if err != nil {
		return err
	}
	return conn.Write(ctx, b)
}

// basicAuth builds an HTTP Basic Authorization header value. Kept local so
// the client has no net/http dependency beyond headers (used for the dial
// request only).
func basicAuth(user, pass string) string {
	// Avoid importing encoding/base64 at the package top to keep imports
	// tidy; the inline use is small and obvious.
	return "Basic " + base64Std(user+":"+pass)
}

// Compile-time interface satisfaction checks.
var (
	_ Dialer  = WSNetDialer{}
	_ Backoff = (*ExponentialBackoff)(nil)
	_ Backoff = FixedBackoff{}
)
