package scripting

import "net/http"

// Request is the mutable request state exposed to scripts as the `request`
// global. Headers are flattened to single-valued map[string]string for JS
// ergonomics (request.headers["X-Foo"] = "bar"); the FromCapture/ApplyTo
// helpers convert to and from the multi-valued http.Header used elsewhere.
type Request struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// Response is the mutable response state exposed to scripts as the `response`
// global.
type Response struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// Exchange bundles the request and response a script may read and mutate. A
// script only touches the halves relevant to its trigger (on_request works on
// request; on_response on response; on_replay/on_webhook on request), but both
// are always bound so a response script can read the originating request.
type Exchange struct {
	Request  Request
	Response Response
}

// normalize ensures the header maps are non-nil so scripts can assign into
// them without a preliminary existence check (and so goja has a concrete map to
// wrap). Called by Run before binding.
func (ex *Exchange) normalize() {
	if ex.Request.Headers == nil {
		ex.Request.Headers = map[string]string{}
	}
	if ex.Response.Headers == nil {
		ex.Response.Headers = map[string]string{}
	}
}

// flattenHeader collapses an http.Header (canonicalised, multi-valued) into a
// single-valued map, joining duplicate values with ", " per RFC 7230 §3.2.2.
func flattenHeader(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, vs := range h {
		switch len(vs) {
		case 0:
			out[k] = ""
		case 1:
			out[k] = vs[0]
		default:
			joined := vs[0]
			for _, v := range vs[1:] {
				joined += ", " + v
			}
			out[k] = joined
		}
	}
	return out
}

// expandHeader converts a single-valued header map back into an http.Header,
// canonicalising keys. Duplicate-valued headers collapsed by flattenHeader are
// not re-split; the joined value round-trips as one field, which is
// semantically equivalent for the headers scripts touch.
func expandHeader(m map[string]string) http.Header {
	h := make(http.Header, len(m))
	for k, v := range m {
		h.Set(k, v)
	}
	return h
}
