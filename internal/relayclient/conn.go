package relayclient

import (
	"context"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// Dialer dials a WebSocket connection to the relay. The default implementation,
// WSNetDialer, wraps coder/websocket.Dial; tests pass a FakeDialer that
// returns an in-memory pipe so the reconnect state machine can be exercised
// without real TCP.
type Dialer interface {
	Dial(ctx context.Context, url string, headers http.Header) (Conn, error)
}

// Conn is the seam the client's protocol loop uses for I/O. The default
// implementation, WSNetConn, adapts *coder/websocket.Conn; tests substitute
// a FakeConn built from paired channels.
type Conn interface {
	// Read blocks until the next message frame or ctx is cancelled.
	// The returned MessageType is currently discarded (relayproto always
	// uses text frames) but kept in the signature for parity with
	// coder/websocket so the production wrapper is a trivial delegation.
	Read(ctx context.Context) (MessageType, []byte, error)
	// Write sends a single message frame. Cancellation is honoured by the
	// underlying *coder/websocket.Conn.Write via its context argument; the
	// mock honours it via select.
	Write(ctx context.Context, b []byte) error
	// Close terminates the connection with a status / reason. Idempotent.
	Close() error
}

// MessageType mirrors websocket.MessageText / MessageBinary. We keep it here
// instead of re-exporting the coder/websocket constants so the test file does
// not need to import the library.
type MessageType int

const (
	MessageText MessageType = iota
	MessageBinary
)

// WSNetDialer is the production Dialer. It delegates to coder/websocket.Dial.
type WSNetDialer struct{}

// Dial implements Dialer. The returned *websocket.Conn is wrapped in a
// WSNetConn to satisfy our Conn interface.
func (WSNetDialer) Dial(ctx context.Context, url string, headers http.Header) (Conn, error) {
	conn, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	return WSNetConn{c: conn}, nil
}

// WSNetConn adapts *coder/websocket.Conn to the Conn interface. Read/Write
// honour ctx by passing it through to the underlying library.
type WSNetConn struct {
	c *websocket.Conn
}

// Read blocks until the next frame. coder/websocket returns a MessageType
// we translate to the local enum. Each Read takes a fresh reader for the
// new frame; ctx cancellation surfaces as an error.
func (w WSNetConn) Read(ctx context.Context) (MessageType, []byte, error) {
	mt, b, err := w.c.Read(ctx)
	if err != nil {
		return 0, nil, err
	}
	switch mt {
	case websocket.MessageText:
		return MessageText, b, nil
	case websocket.MessageBinary:
		return MessageBinary, b, nil
	default:
		return MessageText, b, nil
	}
}

// Write sends b as a text frame — relayproto always encodes JSON, which the
// relay's readMessage expects. Write owns its own 5s timeout so a stuck
// connection does not hold the client's reader goroutine hostage.
func (w WSNetConn) Write(ctx context.Context, b []byte) error {
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return w.c.Write(wctx, websocket.MessageText, b)
}

// Close terminates the WebSocket. coder/websocket accepts a status + reason;
// StatusNormalClosure is the clean-close code that does not trip the peer's
// reconnect logic into an immediate retry burst.
func (w WSNetConn) Close() error {
	return w.c.Close(websocket.StatusNormalClosure, "client shutdown")
}

// base64Std is a tiny alias around encoding/base64.StdEncoding used by
// basicAuth(). Kept local so the package's only consumers of encoding/base64
// are this helper, not scattered call sites.
func base64Std(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}
