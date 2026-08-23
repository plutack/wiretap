package store

import "time"

// Model types represent rows as stored in SQLite. They are intentionally
// separate from relayproto wire types so the storage layer doesn't import
// the wire protocol package; conversion happens in the caller (relayd).
// This keeps SQL concerns and protocol concerns from leaking into each other.

// ClientRow is a row in the relay's `clients` table.
type ClientRow struct {
	ClientID    string
	ClientToken string
	DisplayName string
	CreatedAt   time.Time
	LastSeenAt  time.Time // zero value when NULL
}

// ProjectRow is a row in the relay's `projects` table.
type ProjectRow struct {
	Path      string
	ClientID  string
	CreatedAt time.Time
	AckedSeq  int64
}

// WebhookRow is a row in the webhooks table (both sides use the same shape;
// the PC store omits Delivered/DeliveredAt).
//
// HeadersJSON is the parsed http.Header as JSON (queryable, lossy on order).
// RawHeaders is the raw header block exactly as received by the relay
// (CRLF-joined lines, preserving duplicate headers); used for faithful
// replay and display. Body is the raw request body, byte-exact.
type WebhookRow struct {
	Project     string
	Seq         int64
	ReceivedAt  time.Time
	SourceIP    string
	Method      string
	Path        string
	HeadersJSON string    // parsed http.Header as JSON
	RawHeaders  []byte    // raw header block as received; preserves order+dupes
	Body        []byte    // raw request body, byte-exact
	Delivered   bool      // PC side always false
	DeliveredAt time.Time // PC side always zero
}

// ScriptRow is a row in the local PC's scripts table: a user-authored
// JavaScript payload transformation executed by internal/scripting. Trigger is
// one of on_request/on_response/on_replay/on_webhook; Priority orders chained
// scripts sharing a trigger (lower runs first).
type ScriptRow struct {
	ID        int64
	Name      string
	Trigger   string
	Body      string
	Priority  int
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TrafficCaptureRow is a row in the local PC's traffic_captures table.
// SessionID links the capture to its intercept_sessions row; 0 means "no
// session" (rows captured before sessions existed, or inserted directly).
type TrafficCaptureRow struct {
	ID              int64
	SessionID       int64
	At              time.Time
	Method          string
	URL             string
	ReqHeadersJSON  string
	ReqBody         []byte
	Status          int
	RespHeadersJSON string
	RespBody        []byte
}

// TrafficCaptureSummaryRow contains only the fields needed by capture lists.
// Body lengths come from SQLite's length() function so polling does not load
// potentially large body blobs merely to discard them in the GUI.
type TrafficCaptureSummaryRow struct {
	ID          int64
	SessionID   int64
	At          time.Time
	Method      string
	URL         string
	Status      int
	ReqBodyLen  int
	RespBodyLen int
}

// TrafficCapturePreviewRow contains capture metadata and bounded body prefixes.
// The full body lengths let callers distinguish complete bodies from previews.
type TrafficCapturePreviewRow struct {
	ID              int64
	SessionID       int64
	At              time.Time
	Method          string
	URL             string
	ReqHeadersJSON  string
	ReqBody         []byte
	ReqBodyLen      int
	Status          int
	RespHeadersJSON string
	RespBody        []byte
	RespBodyLen     int
}

// InterceptSessionRow is a row in the local PC's intercept_sessions table:
// one per `wiretap intercept start` run. EndedAt is the zero time while the
// session is running — or when it crashed without cleanup, which the UI can
// render as "not closed cleanly". Captures is a derived count populated by
// InterceptSessions listings, not a stored column.
type InterceptSessionRow struct {
	ID        int64
	StartedAt time.Time
	EndedAt   time.Time // zero while running
	Shell     string
	ProxyAddr string
	Captures  int // derived: COUNT of traffic_captures with this session_id
}
