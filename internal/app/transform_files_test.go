package app

import (
	"strings"
	"testing"
)

func TestTransformFileRoundTrip(t *testing.T) {
	t.Parallel()
	want := TransformDefinition{
		Name: "sign local request", Trigger: "on_replay", Priority: 7,
		Enabled: true, Program: `request.headers["X-Sig"] = "test";`,
	}
	data, err := EncodeTransformFile(want)
	if err != nil {
		t.Fatalf("EncodeTransformFile: %v", err)
	}
	got, err := DecodeTransformFile(data)
	if err != nil {
		t.Fatalf("DecodeTransformFile: %v", err)
	}
	if got.Format != TransformFileFormat || got.Version != TransformFileVersion ||
		got.Name != want.Name || got.Trigger != want.Trigger || got.Priority != want.Priority ||
		got.Enabled != want.Enabled || got.Program != want.Program {
		t.Fatalf("round trip = %+v", got)
	}
}

func TestDecodeTransformFileRejectsUnsafeOrAmbiguousInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, input, contains string
	}{
		{"wrong format", `{"format":"other","version":1,"name":"x","trigger":"on_request","program":""}`, "unsupported transform format"},
		{"future version", `{"format":"wiretap-transform","version":2,"name":"x","trigger":"on_request","program":""}`, "unsupported transform file version"},
		{"invalid trigger", `{"format":"wiretap-transform","version":1,"name":"x","trigger":"sometimes","program":""}`, "invalid trigger"},
		{"missing name", `{"format":"wiretap-transform","version":1,"name":" ","trigger":"on_request","program":""}`, "name is required"},
		{"unknown field", `{"format":"wiretap-transform","version":1,"name":"x","trigger":"on_request","program":"","id":9}`, "unknown field"},
		{"extra value", `{"format":"wiretap-transform","version":1,"name":"x","trigger":"on_request","program":""} {}`, "more than one JSON value"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeTransformFile([]byte(tc.input))
			if err == nil || !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("err = %v, want containing %q", err, tc.contains)
			}
		})
	}
}

func TestDecodeTransformFileRejectsOversizedInput(t *testing.T) {
	t.Parallel()
	_, err := DecodeTransformFile(make([]byte, maxTransformFileSize+1))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v, want size error", err)
	}
}

func TestEncodeTransformFileRejectsOversizedProgram(t *testing.T) {
	t.Parallel()
	_, err := EncodeTransformFile(TransformDefinition{
		Name: "too large", Trigger: "on_request", Program: strings.Repeat("x", maxTransformFileSize),
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v, want size error", err)
	}
}
