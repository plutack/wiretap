package tui

import (
	"strings"
	"testing"

	"github.com/plutack/wiretap/internal/store"
)

func TestRenderBodyAutoPrettyPrintsJSON(t *testing.T) {
	body := []byte(`{"b":1,"a":[1,2]}`)

	got := renderBody(body, "application/json", modeAuto)
	if !strings.Contains(got, "\n  \"a\"") {
		t.Errorf("auto mode should indent JSON, got:\n%s", got)
	}

	// Sniffing works without a Content-Type hint too.
	got = renderBody(body, "", modeAuto)
	if !strings.Contains(got, "\"b\": 1") {
		t.Errorf("sniffed JSON should still pretty-print, got:\n%s", got)
	}
}

func TestRenderBodyAutoPrefersContentType(t *testing.T) {
	// Valid JSON bytes labeled as text must not be reformatted as JSON.
	body := []byte(`12345`)
	got := renderBody(body, "text/plain", modeAuto)
	if strings.ContainsAny(got, "\n") {
		t.Errorf("plain text should render verbatim, got %q", got)
	}
}

func TestRenderBodyAutoHexesBinary(t *testing.T) {
	body := []byte{0x00, 0x01, 0x02, 0xff, 'A'}
	got := renderBody(body, "application/octet-stream", modeAuto)
	if !strings.HasPrefix(got, "00000000") {
		t.Errorf("binary auto mode should hex dump, got %q", got)
	}
	if !strings.Contains(got, "|....A|") {
		t.Errorf("hex ASCII column wrong: %q", got)
	}
}

func TestRenderBodyModes(t *testing.T) {
	body := []byte("line1\n\x01line2")

	if got := renderBody(body, "", modeText); !strings.Contains(got, `\x01`) {
		t.Errorf("text mode should escape controls, got %q", got)
	}
	if got := renderBody(body, "", modeRaw); strings.Contains(got, `\x01`) {
		t.Errorf("raw mode must be verbatim, got %q", got)
	}
	got := renderBody(body, "", modeHex)
	for _, want := range []string{"6c 69 6e 65 31", "01", "line1"} {
		if !strings.Contains(got, want) {
			t.Errorf("hex dump missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderBodyHexLayout(t *testing.T) {
	// 20 bytes → two lines with correct offsets.
	body := make([]byte, 20)
	for i := range body {
		body[i] = byte('a' + i)
	}
	got := renderBody(body, "", modeHex)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("hex lines = %d, want 2:\n%s", len(lines), got)
	}
	if !strings.HasPrefix(lines[0], "00000000") || !strings.HasPrefix(lines[1], "00000010") {
		t.Errorf("offsets wrong: %q / %q", lines[0], lines[1])
	}
	if !strings.Contains(lines[0], "abcdefghijklmnop") {
		t.Errorf("first ASCII column wrong: %q", lines[0])
	}
}

func TestRenderBodyTruncatesHugeInput(t *testing.T) {
	body := make([]byte, maxRenderBody+1024)
	for i := range body {
		body[i] = 'x'
	}
	got := renderBody(body, "text/plain", modeText)
	if !strings.Contains(got, "truncated") {
		t.Error("oversized body should note truncation")
	}
	if len(got) > maxRenderBody+200 {
		t.Errorf("rendered %d bytes, far over the clamp", len(got))
	}
}

func TestBodyModeCycle(t *testing.T) {
	got, want := modeAuto, modeAuto
	for i := 0; i < 4; i++ {
		got = got.next()
	}
	if got != want {
		t.Errorf("after 4 next() calls mode = %v, want %v", got, want)
	}
	if modeAuto.next() != modeText || modeHex.next() != modeAuto {
		t.Error("cycle order should be auto → text → raw → hex → auto")
	}
}

func TestDetailBodyModeSwitch(t *testing.T) {
	wh := mustWebhook(t)
	d := newWebhookDetail(wh, 80, 24)
	if !strings.Contains(d.build(), `"pretty"`) {
		t.Errorf("auto mode should pretty-print the JSON body:\n%s", d.build())
	}
	for i := 0; i < 3; i++ { // auto → text → raw → hex
		d, _ = d.Update(keyPress("m"))
	}
	if !strings.Contains(d.build(), "00000000") {
		t.Errorf("after three m presses the viewer should be in hex mode:\n%s", d.build())
	}
	if d.mode != modeHex {
		t.Errorf("mode = %v, want hex", d.mode)
	}
}

func TestHeaderLinesPrefersRawBlock(t *testing.T) {
	wh := mustWebhook(t)
	lines := headerLines(wh.RawHeaders, wh.HeadersJSON)
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "Host:") {
		t.Fatalf("raw header block should lead with Host:, got %v", lines)
	}

	sorted := headerLines(nil, `{"Z-Last":["1"],"A-First":["2"]}`)
	if len(sorted) != 2 || !strings.HasPrefix(sorted[0], "A-First") {
		t.Errorf("parsed fallback should be alphabetized, got %v", sorted)
	}
}

// mustWebhook builds one webhook row with a JSON body and a raw header block.
func mustWebhook(t *testing.T) *store.WebhookRow {
	t.Helper()
	return &store.WebhookRow{
		Method:      "POST",
		Project:     "proj",
		Seq:         1,
		Path:        "/hook",
		HeadersJSON: `{"Content-Type":["application/json"]}`,
		RawHeaders:  []byte("Host: relay.example.com\r\nContent-Type: application/json\r\n"),
		Body:        []byte(`{"pretty":true}`),
	}
}
