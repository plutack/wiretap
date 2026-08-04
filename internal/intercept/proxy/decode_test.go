package proxy

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"io"
	"net/http"
	"testing"

	"github.com/andybalholm/brotli"
)

func TestDecodeBody(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"hello":"world","n":42}`)

	// identity / empty passthrough
	if got, decoded := decodeBody("", payload); decoded || !bytes.Equal(got, payload) {
		t.Fatalf("identity: decoded=%v got=%q", decoded, got)
	}
	if got, decoded := decodeBody("identity", payload); decoded || !bytes.Equal(got, payload) {
		t.Fatalf("identity token: decoded=%v got=%q", decoded, got)
	}

	// gzip
	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	_, _ = gw.Write(payload)
	_ = gw.Close()
	if got, decoded := decodeBody("gzip", gz.Bytes()); !decoded || !bytes.Equal(got, payload) {
		t.Fatalf("gzip: decoded=%v got=%q want=%q", decoded, got, payload)
	}
	// Case-insensitive + whitespace tolerant.
	if got, decoded := decodeBody("  GZIP ", gz.Bytes()); !decoded || !bytes.Equal(got, payload) {
		t.Fatalf("GZIP: decoded=%v got=%q", decoded, got)
	}

	// deflate (zlib-wrapped)
	var zw bytes.Buffer
	zwr := zlib.NewWriter(&zw)
	_, _ = zwr.Write(payload)
	_ = zwr.Close()
	if got, decoded := decodeBody("deflate", zw.Bytes()); !decoded || !bytes.Equal(got, payload) {
		t.Fatalf("deflate: decoded=%v got=%q want=%q", decoded, got, payload)
	}

	// brotli
	var br bytes.Buffer
	bw := brotli.NewWriter(&br)
	_, _ = bw.Write(payload)
	_ = bw.Close()
	if got, decoded := decodeBody("br", br.Bytes()); !decoded || !bytes.Equal(got, payload) {
		t.Fatalf("br: decoded=%v got=%q want=%q", decoded, got, payload)
	}

	// Unknown encoding → passthrough (do not drop).
	junk := []byte{0x00, 0x01, 0x02}
	if got, decoded := decodeBody("x-fancy", junk); decoded || !bytes.Equal(got, junk) {
		t.Fatalf("unknown: decoded=%v got=%x want=%x", decoded, got, junk)
	}

	// Corrupt gzip → passthrough (don't error).
	corrupt := []byte{0x1f, 0x8b, 0x08, 0x00, 0xff, 0xff, 0xff, 0xff}
	if got, decoded := decodeBody("gzip", corrupt); decoded || !bytes.Equal(got, corrupt) {
		t.Fatalf("corrupt gzip: decoded=%v got=%x want=%x", decoded, got, corrupt)
	}
}

func TestDecodeAndNormalize(t *testing.T) {
	t.Parallel()
	payload := []byte("a realistic response body")

	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	_, _ = gw.Write(payload)
	_ = gw.Close()

	h := http.Header{}
	h.Set("Content-Encoding", "gzip")
	h.Set("Content-Length", "99") // the wire length, not the decoded length

	out, body := decodeAndNormalize(h, gz.Bytes())
	if !bytes.Equal(body, payload) {
		t.Fatalf("body: got %q want %q", body, payload)
	}
	if got := out.Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding not stripped: %q", got)
	}
	if got := out.Get("Content-Length"); got != "" {
		t.Errorf("Content-Length not stripped: %q", got)
	}
	// The caller's original header must be untouched (decodeAndNormalize clones).
	if ce := h.Get("Content-Encoding"); ce != "gzip" {
		t.Errorf("original header mutated: Content-Encoding=%q want gzip", ce)
	}

	// identity passthrough: the same header instance is returned unchanged.
	// A marker header proves no field was added or stripped on the identity path.
	h2 := http.Header{}
	h2.Set("X-Marker", "keep-me")
	out2, body2 := decodeAndNormalize(h2, payload)
	if got := out2.Get("X-Marker"); got != "keep-me" {
		t.Errorf("identity: X-Marker lost: %q", got)
	}
	if got := out2.Get("Content-Encoding"); got != "" {
		t.Errorf("identity: unexpected Content-Encoding=%q", got)
	}
	if !bytes.Equal(body2, payload) {
		t.Fatalf("identity body changed")
	}
}

// A sanity check: gzip round-trips through a real gzip.Reader, so the browser-
// facing byte stream a real server would emit is exactly what we feed decodeBody.
func TestDecodeBody_MatchesGoGzipReader(t *testing.T) {
	t.Parallel()
	want := []byte("brotli and gzip coexist peacefully")
	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	_, _ = gw.Write(want)
	_ = gw.Close()

	// Direct stdlib read for reference.
	ref, err := io.ReadAll(func() io.Reader {
		r, err := gzip.NewReader(bytes.NewReader(gz.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}())
	if err != nil {
		t.Fatal(err)
	}

	got, _ := decodeBody("gzip", gz.Bytes())
	if !bytes.Equal(got, ref) {
		t.Fatalf("decodeBody != gzip.Reader: got %q ref %q", got, ref)
	}
}
