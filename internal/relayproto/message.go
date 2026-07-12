// Package relayproto defines the wire format for the tunnel between the
// local wiretap app and the wiretap-relay server. Messages are JSON objects
// discriminated by a "type" field (a tagged union). The package exposes a
// sealed Message interface: only types declared here can implement it,
// because the interface includes an unexported method.
//
// The package is intentionally pure — no I/O, no network, no clock. That
// makes Encode/Decode trivially table-testable and lets every other package
// (relayclient, relayd) build on a stable, fully-tested contract.
package relayproto

import (
	"encoding/json"
	"fmt"
)

// Type is the message discriminator carried in the JSON "type" field.
type Type string

// Message type constants. Each must match the JSON tag exactly so Decode
// can switch on it.
const (
	TypeHello  Type = "hello"  // PC -> relay
	TypeAck    Type = "ack"    // PC -> relay
	TypeReplay Type = "replay" // PC -> relay
	TypeOK     Type = "ok"     // relay -> PC
	TypePush   Type = "push"   // relay -> PC
	TypeError  Type = "error"  // relay -> PC
)

// Message is the sealed union of all tunnel messages. The messageType
// method is unexported, so only types in this package can satisfy the
// interface — external packages cannot manufacture new Message values.
type Message interface {
	// messageType returns the tag. Sealed by being unexported.
	messageType() Type
}

// Base carries the type tag embedded by every concrete message. Embedding
// Base promotes the messageType method, so each concrete struct satisfies
// Message with no boilerplate.
//
// The `json:"type"` tag means encoding/json writes the discriminator inline
// alongside the concrete struct's own fields, producing a flat object such
// as {"type":"hello","client_id":"..."}.
type Base struct {
	Type Type `json:"type"`
}

// messageType implements Message. The receiver is a value so embedded Base
// promotes it to every concrete type without forcing pointer receivers.
func (b Base) messageType() Type { return b.Type }

// Hello is sent by the PC at tunnel open and on every reconnect. It
// identifies the client and reports the highest sequence number the PC has
// already persisted per project. The relay treats last_seqs as ground truth
// and pushes every webhook with seq greater than the reported value.
type Hello struct {
	Base
	ClientID    string           `json:"client_id"`
	ClientToken string           `json:"client_token"`
	LastSeqs    map[string]int64 `json:"last_seqs,omitempty"`
}

// Ack acknowledges receipt of webhooks up to up_to_seq for a project. The
// relay marks rows delivered and tracks the cursor in projects.acked_seq.
type Ack struct {
	Base
	Project string `json:"project"`
	UpToSeq int64  `json:"up_to_seq"`
}

// Replay asks the relay to re-push specific already-delivered webhooks to
// the local app. Used by the "redeliver to local" feature in the dashboard.
type Replay struct {
	Base
	Project string  `json:"project"`
	Seqs    []int64 `json:"seqs"`
}

// OK is the relay's greeting after a successful Hello. It lists the
// projects the client is authorized for and the seq the relay will resume
// pushing from (which equals the Hello's last_seqs, validated + clamped).
type OK struct {
	Base
	Projects   []string         `json:"projects"`
	ResumeFrom map[string]int64 `json:"resume_from"`
}

// Push delivers a single webhook to the PC.
//
// Headers is the parsed http.Header (map[string][]string) so the local app
// can reconstruct an http.Header directly. RawHeaders is the raw header
// block as received by the relay (CRLF-joined, preserving ordering and
// duplicate headers). Body is the raw request body, byte-exact;
// encoding/json marshals []byte as base64 automatically, preserving
// arbitrary binary payloads. ReceivedAt is a unix-seconds timestamp
// carried through from the relay's store.
type Push struct {
	Base
	Project    string              `json:"project"`
	Seq        int64               `json:"seq"`
	Method     string              `json:"method"`
	Path       string              `json:"path"`
	Headers    map[string][]string `json:"headers"`
	RawHeaders []byte              `json:"raw_headers,omitempty"`
	Body       []byte              `json:"body"`
	ReceivedAt int64               `json:"received_at"`
	SourceIP   string              `json:"source_ip"`
}

// Error is sent for protocol violations (bad auth, unknown project, etc.).
// Code is a stable machine-readable string; Message is human-readable.
type Error struct {
	Base
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Encode serializes m to JSON. The result is suitable for a single WebSocket
// text/binary frame.
func Encode(m Message) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("relayproto: encode: nil message")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("relayproto: encode %s: %w", m.messageType(), err)
	}
	return b, nil
}

// Decode parses a JSON message and returns the concrete Message value. An
// unknown type or malformed JSON yields an error; the caller should close
// the tunnel on error for safety.
//
// Decode performs two unmarshals: the first reads only the type tag, the
// second reads the full payload into the concrete struct. This is cheap
// (webhooks are small) and keeps the API a clean tagged union.
func Decode(b []byte) (Message, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("relayproto: decode: empty payload")
	}
	var env struct {
		Type Type `json:"type"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, fmt.Errorf("relayproto: decode: %w", err)
	}
	switch env.Type {
	case TypeHello:
		var m Hello
		return decodeInto(b, &m)
	case TypeAck:
		var m Ack
		return decodeInto(b, &m)
	case TypeReplay:
		var m Replay
		return decodeInto(b, &m)
	case TypeOK:
		var m OK
		return decodeInto(b, &m)
	case TypePush:
		var m Push
		return decodeInto(b, &m)
	case TypeError:
		var m Error
		return decodeInto(b, &m)
	default:
		return nil, fmt.Errorf("relayproto: decode: unknown type %q", env.Type)
	}
}

// decodeInto is a small helper that finishes a two-pass decode and reports
// the type tag in any error so logs point at the offending message.
//
// It is generic on the concrete message type so json.Unmarshal receives a
// *T (pointer to a concrete struct) rather than a *Message (pointer to an
// interface). The latter would wrap the decoded value as *T inside the
// interface, breaking value equality and type assertions in callers.
func decodeInto[T Message](b []byte, m *T) (Message, error) {
	if err := json.Unmarshal(b, m); err != nil {
		return nil, fmt.Errorf("relayproto: decode %s: %w", any(*m).(Message).messageType(), err)
	}
	return any(*m).(Message), nil
}

// Direction names the half of the tunnel a message may travel.
type Direction int

// Direction constants. iota starts at 0 so DirInvalid is the zero value
// (a useful sentinel for "not set").
const (
	DirInvalid   Direction = iota // zero value; not a valid direction
	DirPCtoRelay                  // PC sends, relay receives
	DirRelayToPC                  // relay sends, PC receives
)

// String implements fmt.Stringer so errors read as "PC->relay" not "1".
func (d Direction) String() string {
	switch d {
	case DirPCtoRelay:
		return "PC->relay"
	case DirRelayToPC:
		return "relay->PC"
	default:
		return "invalid"
	}
}

// dirByType is the source of truth for which direction each type may take.
// Kept as a package-level lookup (not a method) because it catalogs the
// closed set of types and belongs at package scope.
//
//nolint:gochecknoglobals // protocol registry; read-only after init
var dirByType = map[Type]Direction{
	TypeHello:  DirPCtoRelay,
	TypeAck:    DirPCtoRelay,
	TypeReplay: DirPCtoRelay,
	TypeOK:     DirRelayToPC,
	TypePush:   DirRelayToPC,
	TypeError:  DirRelayToPC,
}

// DirectionOf reports which half of the tunnel m may travel. It returns
// DirInvalid for any zero-valued or unknown Base.Type.
func DirectionOf(m Message) Direction {
	if m == nil {
		return DirInvalid
	}
	return dirByType[m.messageType()]
}

// ValidateDirection returns nil if m is permitted in dir, otherwise an
// error naming the offending type and direction. Used by relayd/relayclient
// as a cheap guard against state-machine misuse.
func ValidateDirection(m Message, dir Direction) error {
	if dir == DirInvalid {
		return fmt.Errorf("relayproto: invalid direction %v", dir)
	}
	got := DirectionOf(m)
	if got != dir {
		return fmt.Errorf("relayproto: %s not valid for %s", m.messageType(), dir)
	}
	return nil
}
