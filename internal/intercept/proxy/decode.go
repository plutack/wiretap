// Package proxy — body decoding.
//
// Upstream responses are often transport-compressed (Content-Encoding: gzip,
// deflate, or br) so the bytes a MITM proxy reads off the wire are opaque
// blobs. The proxy records each response for human inspection and hands the
// body to JS scripts (the on_response transformer), both of which only make
// sense against the decoded payload. decodeBody reverses the wire encoding
// so the recorded Capture and any transformer see the logical body.
//
// Unknown encodings (or any decoder error) are passed through untouched so the
// proxy never drops a response: an encoding we can't reverse is still recorded
// honestly (as raw bytes), and the caller can surface the mismatch via the
// Content-Encoding header that stays on the response.
package proxy

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"io"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
)

// decodeBody reverses one hop of Content-Encoding on body, returning the
// decoded bytes. identity (empty), chunked, and anything unrecognized is
// returned as-is. A decode failure also returns the original body unchanged
// rather than aborting the exchange — content negotiation is best-effort for
// an inspector.
func decodeBody(contentEncoding string, body []byte) ([]byte, bool) {
	enc := strings.ToLower(strings.TrimSpace(contentEncoding))
	if enc == "" || enc == "identity" {
		return body, false
	}

	// "gzip, deflate" (comma-listed, though non-conforming) is handled by
	// trying each in order. Real servers send a single value.
	for _, part := range strings.Split(enc, ",") {
		part = strings.TrimSpace(part)
		switch part {
		case "gzip", "x-gzip":
			dec, err := gzip.NewReader(bytes.NewReader(body))
			if err != nil {
				return body, false
			}
			out, err := io.ReadAll(dec)
			_ = dec.Close()
			if err != nil {
				return body, false
			}
			return out, true
		case "deflate", "x-deflate":
			// RFC 7230 says deflate is zlib-wrapped; some servers send raw
			// DEFLATE. Try zlib first, fall back to raw flate.
			out, err := zlibDecompress(body)
			if err == nil {
				return out, true
			}
			out, err = rawDeflateDecompress(body)
			if err == nil {
				return out, true
			}
			return body, false
		case "br":
			out, err := io.ReadAll(brotli.NewReader(bytes.NewReader(body)))
			if err != nil {
				return body, false
			}
			return out, true
		}
	}
	// Unknown/unsupported encoding: keep the wire bytes.
	return body, false
}

func zlibDecompress(body []byte) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

// rawDeflateDecompress handles the non-conformant but common variant where a
// "deflate" Content-Encoding is actually raw DEFLATE (no zlib wrapper). Go's
// compress/flate reads exactly that.
func rawDeflateDecompress(body []byte) ([]byte, error) {
	fr := flate.NewReader(bytes.NewReader(body))
	defer fr.Close()
	return io.ReadAll(fr)
}

// stripContentEncoding removes Content-Encoding and Content-Length from h in
// place. Call this on the headers that accompany a decoded body so consumers
// see a consistent (identity) representation: the recorded body is no longer
// compressed, so claiming gzip with a now-wrong length would mislead.
func stripContentEncoding(h http.Header) {
	h.Del("Content-Encoding")
	h.Del("Content-Length")
}

// decodeAndNormalize is the convenience the proxy's two capture paths
// (bridge + handleHTTP) use: decode the body, and if decoding produced a
// different (decoded) body, strip Content-Encoding/Length from the headers so
// the recorded view and the client-bound view both describe identity. The
// returned header clone carries the change; callers assign it back.
func decodeAndNormalize(h http.Header, body []byte) (http.Header, []byte) {
	out, decoded := decodeBody(h.Get("Content-Encoding"), body)
	if !decoded {
		return h, body
	}
	c := h.Clone()
	stripContentEncoding(c)
	return c, out
}
