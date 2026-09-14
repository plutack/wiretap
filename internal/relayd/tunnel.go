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

// TunnelRegistry indexes each connected client session under all subscribed
// projects. A project may have many sessions; reconnecting replaces only the
// preceding session for that same client identity.
type TunnelRegistry struct {
	mu        sync.RWMutex
	byProject map[string]map[string]*TunnelSession
	byClient  map[string]*TunnelSession
}

// TunnelSession is the relay's side of one open tunnel.
type TunnelSession struct {
	clientID  string
	out       chan relayproto.Message
	done      chan struct{}
	closeOnce sync.Once
	projects  map[string]chan struct{} // project -> coalescing pump wake channel
	mu        sync.RWMutex
}

// NewTunnelRegistry returns an empty registry.
func NewTunnelRegistry() *TunnelRegistry {
	return &TunnelRegistry{
		byProject: make(map[string]map[string]*TunnelSession),
		byClient:  make(map[string]*TunnelSession),
	}
}

func (r *TunnelRegistry) attach(clientID string, projects []string) *TunnelSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old := r.byClient[clientID]; old != nil {
		old.close()
		r.detachLocked(old)
	}
	s := &TunnelSession{
		clientID: clientID,
		out:      make(chan relayproto.Message, 64),
		done:     make(chan struct{}),
		projects: make(map[string]chan struct{}, len(projects)),
	}
	r.byClient[clientID] = s
	for _, project := range projects {
		r.addProjectLocked(s, project)
	}
	return s
}

func (r *TunnelRegistry) sessions(project string) []*TunnelSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	indexed := r.byProject[project]
	out := make([]*TunnelSession, 0, len(indexed))
	for _, session := range indexed {
		out = append(out, session)
	}
	return out
}

func (r *TunnelRegistry) detach(s *TunnelSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byClient[s.clientID] == s {
		r.detachLocked(s)
	}
}

func (r *TunnelRegistry) detachLocked(s *TunnelSession) {
	delete(r.byClient, s.clientID)
	for project := range s.projectWakes() {
		delete(r.byProject[project], s.clientID)
		if len(r.byProject[project]) == 0 {
			delete(r.byProject, project)
		}
	}
}

func (r *TunnelRegistry) addProjectLocked(s *TunnelSession, project string) chan struct{} {
	wake := s.addProject(project)
	if r.byProject[project] == nil {
		r.byProject[project] = make(map[string]*TunnelSession)
	}
	r.byProject[project][s.clientID] = s
	return wake
}

func (s *Server) restartClientTunnel(clientID string) {
	s.tunnels.restartClient(clientID)
}

func (r *TunnelRegistry) restartClient(clientID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.byClient[clientID]
	if s == nil {
		return
	}
	s.close()
	r.detachLocked(s)
}

func (r *TunnelRegistry) removeProject(clientID, project string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.byClient[clientID]
	if s == nil {
		return
	}
	s.removeProject(project)
	delete(r.byProject[project], clientID)
	if len(r.byProject[project]) == 0 {
		delete(r.byProject, project)
	}
	// Reconnect the client so both protocol directions immediately use the
	// authoritative subscription set and no buffered message for the removed
	// project can be delivered afterward.
	s.close()
	r.detachLocked(s)
}

// close stops the session and unblocks anyone waiting on done.
func (s *TunnelSession) close() {
	s.closeOnce.Do(func() {
		// The handler and all delivery pumps select on done. The handler then
		// closes the WebSocket, unblocking its reader goroutine.
		close(s.done)
	})
}

func (s *TunnelSession) send(ctx context.Context, m relayproto.Message) bool {
	select {
	case s.out <- m:
		return true
	case <-s.done:
		return false
	case <-ctx.Done():
		return false
	}
}

func (s *TunnelSession) addProject(project string) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if wake := s.projects[project]; wake != nil {
		return wake
	}
	wake := make(chan struct{}, 1)
	s.projects[project] = wake
	return wake
}

func (s *TunnelSession) removeProject(project string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.projects, project)
}

func (s *TunnelSession) owns(project string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.projects[project]
	return ok
}

func (s *TunnelSession) projectWakes() map[string]chan struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]chan struct{}, len(s.projects))
	for project, wake := range s.projects {
		out[project] = wake
	}
	return out
}

func (s *TunnelSession) notify(project string) {
	s.mu.RLock()
	wake := s.projects[project]
	s.mu.RUnlock()
	if wake == nil {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
}

// countTunnels returns the number of unique connected desktop sessions. One
// session can be indexed under several project paths, so counting map entries
// would overstate the number shown by /health.
func (r *TunnelRegistry) countTunnels() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byClient)
}

// HandleTunnel upgrades the HTTP request to a WebSocket and runs the
// relay-side protocol loop. Auth is HTTP basic auth on the upgrade request
// (client_id / client_token) — coder/websocket pulls headers off the
// http.Request before upgrading.
//
// The flow is:
//  1. PC dials wss://relay/tunnel with HTTP basic auth.
//  2. Relay validates and indexes the session under its subscribed projects.
//  3. PC sends HELLO with last_seqs per project.
//  4. Relay sends OK with resume_from (mirrors last_seqs).
//  5. Relay pumps pending undelivered webhooks (seq > last_seqs) as PUSH.
//  6. PC sends ACK per project; relay advances that subscription's cursor.
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

	// Look up projects subscribed to by this client.
	paths, err := s.store.ProjectsByClient(r.Context(), c.ClientID)
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "store error")
		return
	}
	if len(paths) == 0 {
		_ = conn.Close(websocket.StatusPolicyViolation, "client has no project subscriptions")
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

	// One authenticated connection multiplexes every project subscribed to by
	// this client. Other subscribers to those projects retain their sessions.
	session := s.tunnels.attach(c.ClientID, paths)
	defer s.tunnels.detach(session)

	// Touch last_seen_at.
	_ = s.store.TouchClient(ctx, c.ClientID, s.clock.Now())
	resume := make(map[string]int64, len(paths))
	for _, project := range paths {
		cursor := hello.LastSeqs[project]
		if sub, err := s.store.ProjectSubscription(ctx, project, c.ClientID); err == nil && cursor < sub.StartSeq {
			cursor = sub.StartSeq
		}
		if row, err := s.store.Project(ctx, project); err == nil && cursor >= row.NextSeq {
			cursor = row.NextSeq - 1
		}
		resume[project] = cursor
	}

	// Send OK.
	if err := writeJSONMessage(ctx, conn, relayproto.OK{
		Base:       relayproto.Base{Type: relayproto.TypeOK},
		Projects:   paths,
		ResumeFrom: resume,
	}); err != nil {
		return
	}

	// Start an independent catch-up pump per project. Ingress only wakes these
	// pumps, so slow consumers no longer block ingress or silently lose a push
	// when the outbound channel is temporarily full.
	for _, p := range paths {
		wake := session.addProject(p)
		go s.deliveryPump(ctx, session, p, resume[p], wake)
		session.notify(p)
	}

	// Read loop (PC -> relay): ACK and REPLAY messages.
	go func() {
		s.tunnelReadLoop(ctx, conn, session)
		cancel()
	}()

	// Write loop (relay -> PC): drain session.out until ctx is cancelled.
	for {
		select {
		case <-ctx.Done():
			return
		case <-session.done:
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

func (s *Server) deliveryPump(ctx context.Context, session *TunnelSession, project string, cursor int64, wake <-chan struct{}) {
	const batchSize int64 = 100
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-session.done:
			return
		case <-wake:
		case <-ticker.C:
		}
		if !session.owns(project) {
			return
		}
		for {
			rows, err := s.store.WebhooksAfterLimit(ctx, project, cursor, batchSize)
			if err != nil {
				break
			}
			for _, row := range rows {
				if !session.owns(project) || !session.send(ctx, rowToPush(row)) {
					return
				}
				cursor = row.Seq
			}
			if int64(len(rows)) < batchSize {
				break
			}
		}
	}
}

// tunnelReadLoop reads PC->relay messages. It validates direction and the
// project belongs to the current session, then dispatches ACK and REPLAY.
func (s *Server) tunnelReadLoop(ctx context.Context, conn *websocket.Conn, sess *TunnelSession) {
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
			if !sess.owns(v.Project) {
				continue
			}
			_ = s.store.MarkDeliveredForClient(ctx, v.Project, sess.clientID, v.UpToSeq, s.clock.Now())
		case relayproto.Replay:
			if !sess.owns(v.Project) {
				continue
			}
			// Re-push the listed webhooks to the local app as fresh PUSH
			// messages; it dedups via INSERT OR IGNORE on (project, seq).
			for _, seq := range v.Seqs {
				row, err := s.store.WebhookBySeq(ctx, v.Project, seq)
				if err != nil {
					continue
				}
				_ = sess.send(ctx, rowToPush(*row))
			}
		}
	}
}

// pushIfTunnelAttached wakes every connected subscriber's catch-up pump.
func (s *Server) pushIfTunnelAttached(ctx context.Context, project string, row store.WebhookRow) {
	_ = ctx
	_ = row
	for _, session := range s.tunnels.sessions(project) {
		session.notify(project)
	}
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
