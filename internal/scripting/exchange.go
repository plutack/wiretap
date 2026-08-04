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

// SetRequest loads the request half of ex from HTTP primitives. Headers are
// flattened to single-valued entries (see flattenHeader) and the body is
// converted to a string — adequate for the JSON/webhook payloads scripts touch,
// though a binary body that isn't valid UTF-8 may not round-trip byte-exact.
func (ex *Exchange) SetRequest(method, url string, h http.Header, body []byte) {
	ex.Request = Request{
		Method:  method,
		URL:     url,
		Headers: flattenHeader(h),
		Body:    string(body),
	}
}

// RequestParts returns the (possibly script-mutated) request half as HTTP
// primitives: method, url, canonicalised headers, and body bytes.
func (ex *Exchange) RequestParts() (method, url string, h http.Header, body []byte) {
	return ex.Request.Method, ex.Request.URL, expandHeader(ex.Request.Headers), []byte(ex.Request.Body)
}

// SetResponse loads the response half of ex from HTTP primitives.
func (ex *Exchange) SetResponse(status int, h http.Header, body []byte) {
	ex.Response = Response{
		Status:  status,
		Headers: flattenHeader(h),
		Body:    string(body),
	}
}

// ResponseParts returns the (possibly script-mutated) response half as HTTP
// primitives: status, canonicalised headers, and body bytes.
func (ex *Exchange) ResponseParts() (status int, h http.Header, body []byte) {
	return ex.Response.Status, expandHeader(ex.Response.Headers), []byte(ex.Response.Body)
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
