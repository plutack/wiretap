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
type WebhookRow struct {
	Project     string
	Seq         int64
	ReceivedAt  time.Time
	SourceIP    string
	Method      string
	Path        string
	HeadersJSON string // raw JSON blob; callers (de)serialize
	Body        []byte
	Delivered   bool      // PC side always false
	DeliveredAt time.Time // PC side always zero
}

// TrafficCaptureRow is a row in the local PC's traffic_captures table.
type TrafficCaptureRow struct {
	ID              int64
	At              time.Time
	Method          string
	URL             string
	ReqHeadersJSON  string
	ReqBody         []byte
	Status          int
	RespHeadersJSON string
	RespBody        []byte
}
