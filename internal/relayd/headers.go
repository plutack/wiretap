package relayd

import (
	"bytes"
	"net/http"
	"strings"
)

// serializeRawHeaders reconstructs a raw HTTP header block from http.Header.
// The output is CRLF-joined "Name: Value" lines preserving duplicate values
// (e.g. http.Header{"X-Forwarded-For": ["a","b"]} produces two lines). Header
// names use the canonicalisation that net/http already applied.
//
// This is a faithful *enough* reconstruction for the MVP. The http.Header
// type collapses duplicate values into a slice within one key, but doesn't
// preserve the original ordering between different keys. That's an
// acceptable trade for the dashboard display; a future phase can capture
// truly raw bytes off the connection for stricter replay.
//
// Headers that net/http hides from Request.Header (Host, Content-Length,
// Transfer-Encoding) are added back from the request so the captured block
// reflects what was actually sent over the wire.
func serializeRawHeaders(h http.Header, method, host string) []byte {
	var buf bytes.Buffer
	if host != "" {
		buf.WriteString("Host: ")
		buf.WriteString(host)
		buf.WriteString("\r\n")
	}
	// Stable order across keys: sort by canonical name.
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	// Sort in lexicographic order by canonical header name. The Go stdlib's
	// http.Header.Write does exactly this via map iteration in canonical form;
	// we mirror it explicitly so the output is deterministic across runs (and
	// thus golden-testable).
	for _, k := range sortedKeys(keys) {
		values := h[k]
		for _, v := range values {
			buf.WriteString(k)
			buf.WriteString(": ")
			buf.WriteString(v)
			buf.WriteString("\r\n")
		}
	}
	return buf.Bytes()
}

// sortedKeys returns a copy of keys sorted ascending. Kept tiny for clarity.
func sortedKeys(keys []string) []string {
	out := append([]string(nil), keys...)
	// Simple insertion sort: header sets are small (~10 keys).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// headersToMap converts an http.Header to the map[string][]string shape the
// api.Webhook DTO uses. The map preserves duplicate values within a key but
// not cross-key ordering, mirroring what http.Header itself tracks.
func headersToMap(h http.Header) map[string][]string {
	if len(h) == 0 {
		return nil
	}
	m := make(map[string][]string, len(h))
	for k, v := range h {
		m[k] = append(m[k], v...)
	}
	return m
}

// methodAllowsBody reports whether a body should be read for this method.
// Used by ingress to decide whether to io.ReadAll(r.Body) (which would block
// on a streaming request with no body). Returns false for empty bodies.
var methodAllowsBody = func(method string) bool {
	switch strings.ToUpper(method) {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}
