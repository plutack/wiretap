// Package relayclient is the PC-side of the wiretap relay tunnel. It dials
// the wiretap-relay's /tunnel WebSocket endpoint, sends HELLO with the
// per-project cursor loaded from the local PCStore, receives PUSH messages,
// persists each webhook to PCStore, and sends ACK messages back so the relay
// advances its acked_seq cursor.
//
// The client handles reconnection: when the WebSocket dies (network blip,
// relay restart, etc.) it backs off exponentially (1s -> 30s, jittered), dials
// again, and re-runs HELLO from the cursor the local SQLite dictates — so the
// relay resumes pushing from the right place without dropping or duplicating
// webhooks. Idempotency on the PC side (PK on (project, seq)) makes any
// in-flight duplicate pushes after a reconnect safe to ignore.
//
// Deps are injected so the protocol loop and the reconnect state machine can
// be tested entirely in-memory without spinning up real WebSocket servers.
// The Dialer interface wraps coder/websocket.Dial; tests substitute it with
// a fake that returns a pipe of channels. A full end-to-end test against an
// httptest.Server running internal/relayd lives in client_integration_test.go
// and exercises the wire format the same way Phase 2 did on the relay side.
package relayclient
