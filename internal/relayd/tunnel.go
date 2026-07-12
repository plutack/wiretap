package relayd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/plutack/wiretap/internal/relayproto"
	"github.com/plutack/wiretap/internal/store"
)

// TunnelRegistry keeps the currently-connected tunnels per project so the
// ingress handler can push freshly-stored webhooks straight down to the
// owner's PC instead of waiting for the PC to (re)connect.
//
// A tunnel is a long-lived goroutine reading messages from the PC and a
// push channel for the relay to send messages to the PC. Each project has
// at most one tunnel — when a new tunnel attaches for the same project, we
// close the old one. (Multi-PC sync is a later phase.)
type TunnelRegistry struct {
	mu      sync.RWMutex
	tunnels map[string]*TunnelSession // keyed by project path
}

// TunnelSession is the relay's side of one open tunnel.
type TunnelSession struct {
	project   string
	clientID  string
	out       chan relayproto.Message // relay -> PC (WriteMessage closes if full)
	done      chan struct{}           // closed when the loop exits
	closeOnce sync.Once
}

// NewTunnelRegistry returns an empty registry.
func NewTunnelRegistry() *TunnelRegistry {
	return &TunnelRegistry{tunnels: make(map[string]*TunnelSession)}
}

// attach registers a tunnel for project, replacing any prior tunnel for
// the same project. The returned session's out channel is buffered so the
// ingress path does not block on slow PC consumers.
func (r *TunnelRegistry) attach(project, clientID string) *TunnelSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.tunnels[project]; ok {
		// Politely close the prior tunnel; the new attach wins.
		old.close()
	}
	s := &TunnelSession{
		project:  project,
		clientID: clientID,
		out:      make(chan relayproto.Message, 32),
		done:     make(chan struct{}),
	}
	r.tunnels[project] = s
	return s
}

// lookup returns the active tunnel for project or nil.
func (r *TunnelRegistry) lookup(project string) *TunnelSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tunnels[project]
}

// detach removes the session from the registry if it is still the active one.
// No-op if another tunnel has already replaced it.
func (r *TunnelRegistry) detach(s *TunnelSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cur, ok := r.tunnels[s.project]; ok && cur == s {
		delete(r.tunnels, s.project)
	}
}

// close stops the session and unblocks anyone waiting on done.
func (s *TunnelSession) close() {
	s.closeOnce.Do(func() {
		// Closing out lets the writer loop exit. done is closed by the loop
		// when it returns — but if the writer is blocked on a read, closing
		// out alone won't release it; the run goroutine handles that via ctx.
		// We close done here as well to make detach/idempotent.
		close(s.done)
	})
}

// send pushes a message to the PC. Returns false if the session is gone or
// the buffer is full (caller should treat as dropped for now).
func (s *TunnelSession) send(m relayproto.Message) bool {
	select {
	case s.out <- m:
		return true
	case <-s.done:
		return false
	default:
		// Buffer full — drop. Phase 3 will add backpressure.
		return false
	}
}

// countTunnels returns the number of active tunnels. Used by /health later.
func (r *TunnelRegistry) countTunnels() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tunnels)
}

// HandleTunnel upgrades the HTTP request to a WebSocket and runs the
// relay-side protocol loop. Auth is HTTP basic auth on the upgrade request
// (client_id / client_token) — coder/websocket pulls headers off the
// http.Request before upgrading.
//
// The flow is:
//  1. PC dials wss://relay/tunnel with HTTP basic auth.
//  2. Relay validates, attaches a TunnelSession per owned project.
//  3. PC sends HELLO with last_seqs per project.
//  4. Relay sends OK with resume_from (mirrors last_seqs).
//  5. Relay pumps pending undelivered webhooks (seq > last_seqs) as PUSH.
//  6. PC sends ACK per project; relay marks rows delivered + bumps acked_seq.
//  7. Loop continues: ingress calls pushIfTunnelAttached → PUSH; PC ACKs.
func (s *Server) HandleTunnel(w http.ResponseWriter, r *http.Request) {
	// Validate client creds before upgrading.
	c, err := s.authClientByBasic(r)
	if err != nil {
		status, code, msg := errStatus(err)
		writeErr(w, status, code, msg)
		return
	}
	// Upgrade HTTP -> WebSocket. AcceptOptions defaults reject cross-origin
	// cruft but we run open ingress (PCs are not browsers).
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The PC dials from a CLI; allow any origin.
		InsecureSkipVerify: true,
	})
	if err != nil {
		// Accept already wrote an error response; nothing more to do.
		return
	}
	// We need both directions of this WebSocket: one goroutine reads ACKs and
	// REPLAY messages from the PC, the main goroutine drains session.out to
	// the PC. coder/websocket supports one concurrent reader + one concurrent
	// writer by protocol; we honour that with the tunnelReadLoop goroutine
	// below. CloseRead would cancel all reads and break the protocol, so we
	// keep both sides open.
	defer conn.Close(websocket.StatusInternalError, "internal error")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Look up projects owned by this client.
	paths, err := s.store.ProjectsByClient(r.Context(), c.ClientID)
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "store error")
		return
	}
	if len(paths) == 0 {
		_ = conn.Close(websocket.StatusPolicyViolation, "client owns no projects")
		return
	}

	// Wait for HELLO.
	hello, err := readHello(ctx, conn)
	if err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "expected hello")
		return
	}

	// Don't bother checking that hello.ClientID matches the basic-auth id:
	// they MUST match; if they differ we reject. This catches suspicious
	// clients early.
	if hello.ClientID != c.ClientID {
		_ = conn.Close(websocket.StatusPolicyViolation, "client_id mismatch")
		return
	}

	// Attach a tunnel per project. All share the same outbound channel so the
	// PC multiplexes one stream. (For MVP, projects share one channel per
	// session, keyed by the lowest-path project — simpler than carrying one
	// channel per project. The wire Push carries project so the PC routes.)
	primary := paths[0]
	session := s.tunnels.attach(primary, c.ClientID)
	defer s.tunnels.detach(session)
	// For extra projects, attach under the same session by aliasing the
	// registry to point each path at this session.
	for _, p := range paths[1:] {
		s.tunnels.attachSession(p, session)
	}
	defer func() {
		// Detach aliased projects too so a reconnect starts fresh.
		for _, p := range paths[1:] {
			s.tunnels.detachByProject(p, session)
		}
	}()

	// Touch last_seen_at.
	_ = s.store.TouchClient(ctx, c.ClientID, s.clock.Now())

	// Send OK.
	if err := writeJSONMessage(ctx, conn, relayproto.OK{
		Base:       relayproto.Base{Type: relayproto.TypeOK},
		Projects:   paths,
		ResumeFrom: hello.LastSeqs,
	}); err != nil {
		return
	}

	// Push pending undelivered webhooks per project.
	for _, p := range paths {
		cursor := hello.LastSeqs[p]
		rows, err := s.store.WebhooksAfter(ctx, p, cursor)
		if err != nil {
			continue
		}
		for _, row := range rows {
			_ = session.send(rowToPush(row))
		}
	}

	// Read loop (PC -> relay): ACK and REPLAY messages.
	go s.tunnelReadLoop(ctx, conn, session, paths)

	// Write loop (relay -> PC): drain session.out until ctx is cancelled.
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-session.out:
			if !ok {
				return
			}
			if err := writeJSONMessage(ctx, conn, m); err != nil {
				return
			}
		}
	}
}

// attachSession aliases an existing session under another project path. Used
// when one client owns multiple projects; all paths share the same session.
func (r *TunnelRegistry) attachSession(project string, sess *TunnelSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.tunnels[project]; ok {
		old.close()
	}
	r.tunnels[project] = sess
}

// detachByProject removes the alias iff it still points at sess.
func (r *TunnelRegistry) detachByProject(project string, sess *TunnelSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cur, ok := r.tunnels[project]; ok && cur == sess {
		delete(r.tunnels, project)
	}
}

// tunnelReadLoop reads PC->relay messages. It validates direction and the
// project is owned by the current session, then dispatches ACK and REPLAY.
func (s *Server) tunnelReadLoop(ctx context.Context, conn *websocket.Conn, sess *TunnelSession, ownedPaths []string) {
	for {
		m, err := readMessage(ctx, conn)
		if err != nil {
			return
		}
		// Direction validation: only PC->relay messages are legal here.
		if err := relayproto.ValidateDirection(m, relayproto.DirPCtoRelay); err != nil {
			continue
		}
		switch v := m.(type) {
		case relayproto.Ack:
			if !s.ownsProject(ownedPaths, v.Project) {
				continue
			}
			_ = s.store.MarkDelivered(ctx, v.Project, v.UpToSeq, s.clock.Now())
		case relayproto.Replay:
			if !s.ownsProject(ownedPaths, v.Project) {
				continue
			}
			// Re-push the listed webhooks. Phase 3 will surface this to the
			// local app; for now we just re-send over the session.
			for _, seq := range v.Seqs {
				row, err := s.store.WebhookBySeq(ctx, v.Project, seq)
				if err != nil {
					continue
				}
				_ = sess.send(rowToPush(*row))
			}
		}
	}
}

func (s *Server) ownsProject(paths []string, p string) bool {
	for _, x := range paths {
		if x == p {
			return true
		}
	}
	return false
}

// pushIfTunnelAttached forwards a freshly-stored webhook to the owning
// client's tunnel if one is connected. No-op when no tunnel is attached;
// the webhook sits in the relay's SQLite until the next reconnect.
//
// This is the implementation referenced from server.go's handleIngress.
func (s *Server) pushIfTunnelAttached(ctx context.Context, project string, row store.WebhookRow) {
	sess := s.tunnels.lookup(project)
	if sess == nil {
		return
	}
	_ = sess.send(rowToPush(row))
}

// readHello blocks until a HELLO message arrives or ctx is cancelled.
func readHello(ctx context.Context, conn *websocket.Conn) (*relayproto.Hello, error) {
	m, err := readMessage(ctx, conn)
	if err != nil {
		return nil, err
	}
	h, ok := m.(relayproto.Hello)
	if !ok {
		return nil, fmt.Errorf("tunnel: expected hello, got %T", m)
	}
	return &h, nil
}

// readMessage reads one JSON message frame from the WebSocket.
func readMessage(ctx context.Context, conn *websocket.Conn) (relayproto.Message, error) {
	_, b, err := conn.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("tunnel: read: %w", err)
	}
	m, err := relayproto.Decode(b)
	if err != nil {
		return nil, fmt.Errorf("tunnel: decode: %w", err)
	}
	return m, nil
}

// writeJSONMessage encodes and writes one message frame as text. 5s write
// deadline keeps a stuck PC from holding the relay's writers hostage.
func writeJSONMessage(ctx context.Context, conn *websocket.Conn, m relayproto.Message) error {
	b, err := relayproto.Encode(m)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, b)
}

// rowToPush converts a stored webhook into an outbound Push message. The
// wire shape uses map[string][]string for headers; we parse the stored JSON
// blob lazily here.
func rowToPush(row store.WebhookRow) relayproto.Push {
	var headers map[string][]string
	_ = json.Unmarshal([]byte(row.HeadersJSON), &headers)
	return relayproto.Push{
		Base:       relayproto.Base{Type: relayproto.TypePush},
		Project:    row.Project,
		Seq:        row.Seq,
		Method:     row.Method,
		Path:       row.Path,
		Headers:    headers,
		RawHeaders: row.RawHeaders,
		Body:       row.Body,
		ReceivedAt: row.ReceivedAt.Unix(),
		SourceIP:   row.SourceIP,
	}
}

// Compile-time assertion that *Server satisfies whatever interface surface
// we expose for tests to inspect tunnel state. Currently a no-op interface.
var _ interface{ unused() } = (*Server)(nil)

func (s *Server) unused() {}

var _ = errors.New // reserved for future error construction
