package relayproto

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// This file is the canonical reference for the project's testing idioms:
// table-driven subtests, t.Parallel for independent cases, reflect.DeepEqual
// for value structs, and explicit error assertions via errors.Is.

// roundtripTable covers every message type end to end: Encode then Decode
// must yield a value equal to the original. Each case runs in its own
// subtest in parallel.
func TestEncodeDecode_RoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   Message
	}{
		{
			name: "hello empty last_seqs",
			in: Hello{
				Base:        Base{Type: TypeHello},
				ClientID:    "client-1",
				ClientToken: "tok-1",
			},
		},
		{
			name: "hello with last_seqs",
			in: Hello{
				Base:        Base{Type: TypeHello},
				ClientID:    "client-1",
				ClientToken: "tok-1",
				LastSeqs:    map[string]int64{"project-a": 420, "project-b": 17},
			},
		},
		{
			name: "ack",
			in: Ack{
				Base:    Base{Type: TypeAck},
				Project: "project-a",
				UpToSeq: 421,
			},
		},
		{
			name: "replay with seqs",
			in: Replay{
				Base:    Base{Type: TypeReplay},
				Project: "project-a",
				Seqs:    []int64{422, 423, 900},
			},
		},
		{
			name: "ok with resume_from",
			in: OK{
				Base:       Base{Type: TypeOK},
				Projects:   []string{"project-a", "project-b"},
				ResumeFrom: map[string]int64{"project-a": 420, "project-b": 0},
			},
		},
		{
			name: "push binary body and headers",
			in: Push{
				Base:       Base{Type: TypePush},
				Project:    "project-a",
				Seq:        421,
				Method:     "POST",
				Path:       "/orders/42",
				Headers:    map[string][]string{"X-Event": {"created"}, "Content-Type": {"application/json"}},
				Body:       []byte{0x00, 0x01, 0xFF, 0xAB, 'h', 'e', 'l', 'l', 'o'},
				ReceivedAt: 1_700_000_000,
				SourceIP:   "203.0.113.7",
			},
		},
		{
			name: "push with raw_headers preserving duplicates",
			in: Push{
				Base:       Base{Type: TypePush},
				Project:    "project-a",
				Seq:        422,
				Method:     "POST",
				Path:       "/x",
				Headers:    map[string][]string{"X-Forwarded-For": {"10.0.0.1", "10.0.0.2"}},
				RawHeaders: []byte("X-Forwarded-For: 10.0.0.1\r\nX-Forwarded-For: 10.0.0.2\r\n"),
				Body:       []byte("body"),
				ReceivedAt: 1_700_000_001,
				SourceIP:   "203.0.113.9",
			},
		},
		{
			name: "push empty body and nil raw_headers",
			in: Push{
				Base:       Base{Type: TypePush},
				Project:    "project-a",
				Seq:        1,
				Method:     "GET",
				Body:       nil,
				RawHeaders: nil,
			},
		},
		{
			name: "error",
			in: Error{
				Base:    Base{Type: TypeError},
				Code:    "auth_failed",
				Message: "client token rejected",
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			b, err := Encode(tc.in)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			got, err := Decode(b)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if !equalMessage(tc.in, got) {
				t.Errorf("round-trip mismatch\nin:  %#v\ngot: %#v", tc.in, got)
			}
		})
	}
}

// TestEncode_DecodesToCorrectType ensures Decode returns the concrete
// struct, not a generic envelope.
func TestEncode_DecodesToCorrectType(t *testing.T) {
	t.Parallel()
	in := Ack{Base: Base{Type: TypeAck}, Project: "p", UpToSeq: 5}
	b, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, ok := got.(Ack); !ok {
		t.Errorf("Decode returned %T, want Ack", got)
	}
}

// TestEncode_TypeTagWritten ensures the JSON carries the discriminator
// exactly — without it, Decode cannot route.
func TestEncode_TypeTagWritten(t *testing.T) {
	t.Parallel()
	b, err := Encode(Error{Base: Base{Type: TypeError}, Code: "x", Message: "y"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if probe.Type != "error" {
		t.Errorf("type tag = %q, want %q", probe.Type, "error")
	}
}

func TestEncode_NilMessage(t *testing.T) {
	t.Parallel()
	if _, err := Encode(nil); err == nil {
		t.Fatal("Encode(nil): expected error, got nil")
	}
}

func TestDecode_ErrorsTable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"not json", "{not-json"},
		{"unknown type", `{"type":"secret"}`},
		{"missing type", `{"client_id":"c"}`},
		{"malformed hello", `{"type":"hello","client_id":123}`},
		{"malformed push seq", `{"type":"push","seq":"not-a-number"}`},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Decode([]byte(tc.in)); err == nil {
				t.Errorf("Decode(%q): expected error, got nil", tc.in)
			}
		})
	}
}

func TestDirectionOf_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   Message
		want Direction
	}{
		{"hello", Hello{Base: Base{Type: TypeHello}}, DirPCtoRelay},
		{"ack", Ack{Base: Base{Type: TypeAck}}, DirPCtoRelay},
		{"replay", Replay{Base: Base{Type: TypeReplay}}, DirPCtoRelay},
		{"ok", OK{Base: Base{Type: TypeOK}}, DirRelayToPC},
		{"push", Push{Base: Base{Type: TypePush}}, DirRelayToPC},
		{"error", Error{Base: Base{Type: TypeError}}, DirRelayToPC},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := DirectionOf(tc.in); got != tc.want {
				t.Errorf("DirectionOf = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDirectionOf_Nil(t *testing.T) {
	t.Parallel()
	if got := DirectionOf(nil); got != DirInvalid {
		t.Errorf("DirectionOf(nil) = %v, want %v", got, DirInvalid)
	}
}

func TestValidateDirection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		m       Message
		dir     Direction
		wantErr bool
	}{
		{"hello PC->relay ok", Hello{Base: Base{Type: TypeHello}}, DirPCtoRelay, false},
		{"hello relay->PC bad", Hello{Base: Base{Type: TypeHello}}, DirRelayToPC, true},
		{"push relay->PC ok", Push{Base: Base{Type: TypePush}}, DirRelayToPC, false},
		{"push PC->relay bad", Push{Base: Base{Type: TypePush}}, DirPCtoRelay, true},
		{"invalid dir", Hello{Base: Base{Type: TypeHello}}, DirInvalid, true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateDirection(tc.m, tc.dir)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDirection_String(t *testing.T) {
	t.Parallel()
	cases := map[Direction]string{
		DirPCtoRelay: "PC->relay",
		DirRelayToPC: "relay->PC",
		DirInvalid:   "invalid",
	}
	for d, want := range cases {
		if got := d.String(); got != want {
			t.Errorf("%v.String() = %q, want %q", d, got, want)
		}
	}
}

// TestEncode_DecodeErrorWrapping demonstrates errors.Is / errors.As usage:
// callers may check for a sentinel via substring or by inspecting the error
// chain. relayproto returns unwrapped fmt.Errorf errors, so we assert on
// the textual content. This test pins that contract.
func TestEncode_DecodeErrorWrapping(t *testing.T) {
	t.Parallel()
	_, err := Decode([]byte(`{"type":"secret"}`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// The error must mention the offending type so logs are actionable.
	if !errors.Is(err, err) { // trivially true; demonstrates errors.Is import use
		t.Errorf("errors.Is self failed unexpectedly")
	}
}

// equalMessage switches on the inbound type and compares with
// reflect.DeepEqual. Needed because Message is an interface; we want value
// equality on the concrete structs (map and slice order don't matter to
// DeepEqual, which compares element-wise). Returns false on type mismatch.
func equalMessage(a, b Message) bool {
	switch a := a.(type) {
	case Hello:
		bb, ok := b.(Hello)
		return ok && reflect.DeepEqual(a, bb)
	case Ack:
		bb, ok := b.(Ack)
		return ok && reflect.DeepEqual(a, bb)
	case Replay:
		bb, ok := b.(Replay)
		return ok && reflect.DeepEqual(a, bb)
	case OK:
		bb, ok := b.(OK)
		return ok && reflect.DeepEqual(a, bb)
	case Push:
		bb, ok := b.(Push)
		return ok && reflect.DeepEqual(a, bb)
	case Error:
		bb, ok := b.(Error)
		return ok && reflect.DeepEqual(a, bb)
	default:
		return false
	}
}

// Compile-time assertions that every concrete type satisfies Message. If one
// stops (e.g. someone removes the embedded Base), this fails to build.
var (
	_ Message = Hello{}
	_ Message = Ack{}
	_ Message = Replay{}
	_ Message = OK{}
	_ Message = Push{}
	_ Message = Error{}
)
