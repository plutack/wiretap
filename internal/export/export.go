// Package export renders captured HTTP requests as ready-to-run code
// snippets (curl, fetch, python-requests, go, …) by embedding Kong's
// httpsnippet (via the isomorphic httpsnippet-lite build) and executing it
// in-process with goja — the same pure-Go JS interpreter internal/scripting
// already uses, so no Node.js and no CGO.
//
// The JS side is a single committed esbuild bundle (js/httpsnippet.bundle.js,
// rebuilt with `make snippet-bundle`); see js/entry.js for the two globals it
// exposes. The package is I/O-free: callers (internal/app) convert store rows
// into a Request and receive a string back, which keeps the engine trivially
// testable and shared by the CLI, TUI, and GUI alike.
//
// httpsnippet's input format is a HAR request object
// (http://www.softwareishard.com/blog/har-12-spec/#request); buildHAR maps
// Request onto it.
package export

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/require"
	"github.com/dop251/goja_nodejs/url"
)

//go:embed js/httpsnippet.bundle.js
var bundleSrc string

// runTimeout aborts a conversion that somehow runs away. Conversions are
// pure string manipulation and finish in microseconds; the interrupt is a
// safety net, mirroring internal/scripting's per-run timeout.
const runTimeout = 5 * time.Second

// Request is the exportable projection of a captured exchange: just the
// request half, since a snippet reproduces what was *sent*. Headers use the
// http.Header shape (name -> values) exactly as rows store them.
type Request struct {
	Method  string
	URL     string
	Headers map[string][]string
	Body    []byte
}

// Target describes one snippet language (httpsnippet "target") and its
// available clients. DefaultClient is used when a caller passes an empty
// client key.
type Target struct {
	Key           string   `json:"key"`
	Title         string   `json:"title"`
	DefaultClient string   `json:"default"`
	Clients       []Client `json:"clients"`
}

// Client is one concrete library/tool within a Target (e.g. shell/curl,
// javascript/fetch).
type Client struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// Engine state: the bundle is compiled once (the expensive part) and each
// call gets a fresh goja.Runtime, so conversions never share JS state and
// the package is safe for concurrent use. The require registry is shared —
// it only backs the URL polyfill and is designed for cross-runtime reuse.
var (
	compileOnce sync.Once
	program     *goja.Program
	compileErr  error
	registry    = new(require.Registry)
)

func compiledBundle() (*goja.Program, error) {
	compileOnce.Do(func() {
		program, compileErr = goja.Compile("httpsnippet.bundle.js", bundleSrc, false)
	})
	return program, compileErr
}

// newRuntime builds a fresh runtime with the URL polyfill (goja lacks the
// WHATWG URL global httpsnippet-lite parses request URLs with) and the
// bundle evaluated.
func newRuntime() (*goja.Runtime, error) {
	prog, err := compiledBundle()
	if err != nil {
		return nil, fmt.Errorf("export: compile bundle: %w", err)
	}
	vm := goja.New()
	registry.Enable(vm)
	url.Enable(vm)
	timer := time.AfterFunc(runTimeout, func() {
		vm.Interrupt("export: conversion timed out")
	})
	defer timer.Stop()
	if _, err := vm.RunProgram(prog); err != nil {
		return nil, fmt.Errorf("export: evaluate bundle: %w", err)
	}
	return vm, nil
}

// Snippet renders req as a code snippet for the given target (language) and
// client (library). An empty client selects the target's default. Unknown
// targets/clients return the httpsnippet error message.
func Snippet(req Request, target, client string) (string, error) {
	if target == "" {
		return "", fmt.Errorf("export: target is required")
	}
	har, err := json.Marshal(buildHAR(req))
	if err != nil {
		return "", fmt.Errorf("export: marshal HAR: %w", err)
	}

	vm, err := newRuntime()
	if err != nil {
		return "", err
	}
	timer := time.AfterFunc(runTimeout, func() {
		vm.Interrupt("export: conversion timed out")
	})
	defer timer.Stop()

	// Drive the conversion through globals + one script so goja fully drains
	// its promise job queue before we read the outcome: reactions run when
	// the top-level script exits, not inside the wiretapSnippet call itself.
	vm.Set("__har", string(har))
	vm.Set("__target", target)
	vm.Set("__client", client)
	if _, err := vm.RunString(`var __out, __err;
		wiretapSnippet(__har, __target, __client).then(
			function (v) { __out = v; },
			function (e) { __err = String((e && e.message) || e); });`); err != nil {
		return "", fmt.Errorf("export: convert: %w", err)
	}
	if v := vm.Get("__err"); v != nil && !goja.IsUndefined(v) {
		return "", fmt.Errorf("export: convert %s/%s: %s", target, client, v.String())
	}
	out := vm.Get("__out")
	if out == nil || goja.IsUndefined(out) {
		return "", fmt.Errorf("export: convert %s/%s: no result", target, client)
	}
	return out.String(), nil
}

// Targets returns the catalog of snippet languages and clients, in
// httpsnippet's stable ordering (alphabetical by target key).
func Targets() ([]Target, error) {
	vm, err := newRuntime()
	if err != nil {
		return nil, err
	}
	v, err := vm.RunString("wiretapTargets()")
	if err != nil {
		return nil, fmt.Errorf("export: list targets: %w", err)
	}
	var out []Target
	if err := json.Unmarshal([]byte(v.String()), &out); err != nil {
		return nil, fmt.Errorf("export: parse targets: %w", err)
	}
	return out, nil
}

// --- HAR mapping ----------------------------------------------------------

// harRequest is the subset of the HAR 1.2 request object httpsnippet reads.
// queryString stays empty: httpsnippet parses the URL's own query into its
// snippet output, and duplicating it here would double the parameters.
type harRequest struct {
	Method      string   `json:"method"`
	URL         string   `json:"url"`
	HTTPVersion string   `json:"httpVersion"`
	Headers     []harNV  `json:"headers"`
	QueryString []harNV  `json:"queryString"`
	Cookies     []harNV  `json:"cookies"`
	PostData    *harPost `json:"postData,omitempty"`
	HeadersSize int      `json:"headersSize"`
	BodySize    int      `json:"bodySize"`
}

type harNV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harPost struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

// buildHAR converts a Request into the HAR shape. Hop-by-hop and
// length-style headers are dropped — they describe the original transport,
// and tools regenerate them (curl computes Content-Length itself; a stale
// one breaks the replayed request after the user edits the body).
func buildHAR(req Request) harRequest {
	h := harRequest{
		Method:      req.Method,
		URL:         req.URL,
		HTTPVersion: "HTTP/1.1",
		Headers:     []harNV{},
		QueryString: []harNV{},
		Cookies:     []harNV{},
		HeadersSize: -1,
		BodySize:    -1,
	}
	mime := ""
	for name, values := range req.Headers {
		if skipHeader(name) {
			continue
		}
		for _, v := range values {
			h.Headers = append(h.Headers, harNV{Name: name, Value: v})
		}
		if http.CanonicalHeaderKey(name) == "Content-Type" && len(values) > 0 {
			mime = values[0]
		}
	}
	// Map iteration order is random; keep snippets deterministic.
	sortHeaders(h.Headers)
	if len(req.Body) > 0 {
		if mime == "" {
			mime = "application/octet-stream"
		}
		h.PostData = &harPost{MimeType: mime, Text: string(req.Body)}
	}
	return h
}

// skipHeader reports whether a header should be omitted from snippets:
// hop-by-hop headers plus lengths the generated code recomputes.
func skipHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"Proxy-Connection", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
		"Content-Length":
		return true
	default:
		return false
	}
}

func sortHeaders(hs []harNV) {
	sort.Slice(hs, func(i, j int) bool {
		if hs[i].Name != hs[j].Name {
			return hs[i].Name < hs[j].Name
		}
		return hs[i].Value < hs[j].Value
	})
}
