package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// bodyMode is one content-aware viewer mode, mirroring the GUI's code-block
// (ui/components/code-block.js) minus the image mode, which has no portable
// terminal equivalent.
type bodyMode int

const (
	modeAuto bodyMode = iota // follow content: pretty JSON, text, or hex
	modeText                 // printable text, non-printables escaped
	modeRaw                  // verbatim bytes as a string
	modeHex                  // offset + hex + ASCII dump
)

var bodyModeNames = map[bodyMode]string{
	modeAuto: "auto",
	modeText: "text",
	modeRaw:  "raw",
	modeHex:  "hex",
}

func (m bodyMode) String() string { return bodyModeNames[m] }

// next cycles auto → text → raw → hex → auto.
func (m bodyMode) next() bodyMode {
	return (m + 1) % (modeHex + 1)
}

// maxRenderBody caps how many body bytes the viewer renders. The GUI warns
// past 15 MB; a terminal viewport never wants that much, so hex dumps and
// pretty-prints are clamped with a trailing notice instead.
const maxRenderBody = 256 * 1024

// renderBody produces the viewer text for body under mode. contentType is the
// matching Content-Type header (may be empty), used by the auto mode.
func renderBody(body []byte, contentType string, mode bodyMode) string {
	clamped := false
	if len(body) > maxRenderBody {
		body = body[:maxRenderBody]
		clamped = true
	}
	var b strings.Builder
	switch mode {
	case modeAuto:
		if isJSON(body, contentType) {
			if pretty := prettyJSON(body); pretty != "" {
				b.WriteString(pretty)
			} else {
				b.Write(body)
			}
		} else if isMostlyPrintable(body) {
			b.WriteString(printable(body))
		} else {
			writeHex(&b, body)
		}
	case modeText:
		b.WriteString(printable(body))
	case modeRaw:
		b.Write(body)
	case modeHex:
		writeHex(&b, body)
	}
	if clamped {
		fmt.Fprintf(&b, "\n… body truncated at %s for display\n", byteCount(maxRenderBody))
	}
	return b.String()
}

// isJSON reports whether body should pretty-print: the Content-Type says so,
// or the bytes sniff as a `{`/`[`-rooted JSON document (a bare number or
// string technically parses but is almost always plain text).
func isJSON(body []byte, contentType string) bool {
	if len(body) == 0 {
		return false
	}
	trimmed := strings.TrimLeft(string(body), " \t\r\n")
	if strings.Contains(strings.ToLower(contentType), "json") {
		return json.Valid(body)
	}
	return (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) && json.Valid(body)
}

// prettyJSON indents valid JSON; returns "" when indentation fails (caller
// falls back to the raw bytes).
func prettyJSON(body []byte) string {
	var out bytes.Buffer
	if err := json.Indent(&out, body, "", "  "); err != nil {
		return ""
	}
	return out.String()
}

// isMostlyPrintable decides auto-mode text vs hex the way the GUI's auto mode
// effectively does: decodable UTF-8 without control garbage reads as text.
func isMostlyPrintable(body []byte) bool {
	if len(body) == 0 {
		return true
	}
	if !utf8.Valid(body) {
		return false
	}
	control, printable := 0, 0
	for _, r := range string(body) {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
			// structural whitespace still counts as text
		case r < 0x20 || r == 0x7f:
			control++
		default:
			printable++
		}
	}
	return control*10 <= printable
}

// printable renders body as text, escaping C0 controls (except newline and
// tab) so binary junk cannot scramble the viewport.
func printable(body []byte) string {
	if utf8.Valid(body) {
		var b strings.Builder
		for _, r := range string(body) {
			switch {
			case r == '\n' || r == '\t':
				b.WriteRune(r)
			case r < 0x20 || r == 0x7f:
				b.WriteString(fmt.Sprintf("\\x%02x", r))
			default:
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	// Invalid UTF-8: degrade per byte.
	var b strings.Builder
	for _, c := range body {
		switch {
		case c == '\n' || c == '\t':
			b.WriteByte(c)
		case c < 0x20 || c == 0x7f || c >= utf8.RuneSelf:
			b.WriteString(fmt.Sprintf("\\x%02x", c))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// writeHex emits a classic hex dump — 16 bytes per line with an 8-digit
// offset and an ASCII column — the terminal twin of code-block.js's hex mode.
func writeHex(b *strings.Builder, body []byte) {
	for off := 0; off < len(body); off += 16 {
		end := off + 16
		if end > len(body) {
			end = len(body)
		}
		chunk := body[off:end]
		fmt.Fprintf(b, "%08x  ", off)
		for i := 0; i < 16; i++ {
			if i == 8 {
				b.WriteByte(' ')
			}
			if i < len(chunk) {
				fmt.Fprintf(b, "%02x ", chunk[i])
			} else {
				b.WriteString("   ")
			}
		}
		b.WriteString(" |")
		for _, c := range chunk {
			if c >= 0x20 && c < 0x7f {
				b.WriteByte(c)
			} else {
				b.WriteByte('.')
			}
		}
		b.WriteString("|\n")
	}
}

// byteCount formats a byte count the way the GUI's fmtBytes does (KiB/MiB).
func byteCount(n int) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
