package scripting

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

// run is a tiny helper: fresh engine, background context, single script.
func run(t *testing.T, script string, ex *Exchange) (Result, error) {
	t.Helper()
	return New().Run(context.Background(), script, ex)
}

func TestTrigger_Valid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		trig Trigger
		want bool
	}{
		{"on_request", OnRequest, true},
		{"on_response", OnResponse, true},
		{"on_replay", OnReplay, true},
		{"on_webhook", OnWebhook, true},
		{"empty", Trigger(""), false},
		{"unknown", Trigger("on_boot"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.trig.Valid(); got != tc.want {
				t.Fatalf("Valid() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRun_MutatesRequest(t *testing.T) {
	t.Parallel()
	ex := &Exchange{Request: Request{
		Method:  "GET",
		URL:     "http://example.com/old",
		Headers: map[string]string{"X-Keep": "1", "X-Drop": "2"},
		Body:    "hello world",
	}}
	script := `
		request.method = "POST";
		request.url = "http://example.com/new";
		request.headers["X-Added"] = "yes";
		request.headers["X-Keep"] = "updated";
		delete request.headers["X-Drop"];
		request.body = request.body.replace("world", "wiretap");
	`
	if _, err := run(t, script, ex); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ex.Request.Method != "POST" {
		t.Errorf("method = %q, want POST", ex.Request.Method)
	}
	if ex.Request.URL != "http://example.com/new" {
		t.Errorf("url = %q", ex.Request.URL)
	}
	if ex.Request.Body != "hello wiretap" {
		t.Errorf("body = %q, want %q", ex.Request.Body, "hello wiretap")
	}
	want := map[string]string{"X-Keep": "updated", "X-Added": "yes"}
	if !reflect.DeepEqual(ex.Request.Headers, want) {
		t.Errorf("headers = %v, want %v", ex.Request.Headers, want)
	}
}

func TestRun_MutatesResponse(t *testing.T) {
	t.Parallel()
	ex := &Exchange{Response: Response{Status: 200, Body: "{}"}}
	script := `
		response.status = 503;
		response.headers["Retry-After"] = "30";
		response.body = JSON.stringify({error: "unavailable"});
	`
	if _, err := run(t, script, ex); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ex.Response.Status != 503 {
		t.Errorf("status = %d, want 503", ex.Response.Status)
	}
	if ex.Response.Headers["Retry-After"] != "30" {
		t.Errorf("Retry-After = %q", ex.Response.Headers["Retry-After"])
	}
	if ex.Response.Body != `{"error":"unavailable"}` {
		t.Errorf("body = %q", ex.Response.Body)
	}
}

func TestRun_ResponseScriptCanReadRequest(t *testing.T) {
	t.Parallel()
	ex := &Exchange{
		Request:  Request{Method: "POST", URL: "http://x/y"},
		Response: Response{Status: 200},
	}
	if _, err := run(t, `response.headers["X-Echo-Method"] = request.method;`, ex); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ex.Response.Headers["X-Echo-Method"] != "POST" {
		t.Errorf("echo = %q, want POST", ex.Response.Headers["X-Echo-Method"])
	}
}

func TestRun_Reject(t *testing.T) {
	t.Parallel()
	ex := &Exchange{Request: Request{Body: "spam"}}
	res, err := run(t, `if (request.body === "spam") { reject("no spam"); }`, ex)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Rejected || res.RejectReason != "no spam" {
		t.Fatalf("Result = %+v, want rejected with reason", res)
	}
}

func TestRun_ConsoleCaptured(t *testing.T) {
	t.Parallel()
	res, err := run(t, `console.log("a", 1, true); console.error("boom");`, &Exchange{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"a 1 true", "boom"}
	if !reflect.DeepEqual(res.Logs, want) {
		t.Fatalf("Logs = %v, want %v", res.Logs, want)
	}
}

func TestRun_Builtins(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		script string
		want   string // written into request.body by the script
	}{
		{
			name:   "hmac sha256",
			script: `request.body = crypto.hmac("sha256", "key", "The quick brown fox jumps over the lazy dog");`,
			want:   "f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8",
		},
		{
			name:   "sha256",
			script: `request.body = crypto.sha256("abc");`,
			want:   "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		},
		{
			name:   "sha1",
			script: `request.body = crypto.sha1("abc");`,
			want:   "a9993e364706816aba3e25717850c26c9cd0d89d",
		},
		{
			name:   "base64 roundtrip",
			script: `request.body = base64.decode(base64.encode("wiretap!"));`,
			want:   "wiretap!",
		},
		{
			name:   "base64 encode",
			script: `request.body = base64.encode("hi");`,
			want:   "aGk=",
		},
		{
			name:   "regex match true",
			script: `request.body = String(regex.match("^\\d+$", "12345"));`,
			want:   "true",
		},
		{
			name:   "regex match false",
			script: `request.body = String(regex.match("^\\d+$", "12a"));`,
			want:   "false",
		},
		{
			name:   "regex replace",
			script: `request.body = regex.replace("o", "foo", "0");`,
			want:   "f00",
		},
		{
			name:   "json roundtrip",
			script: `request.body = json.stringify(json.parse('{"n":42}'));`,
			want:   `{"n":42}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ex := &Exchange{}
			if _, err := run(t, tc.script, ex); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if ex.Request.Body != tc.want {
				t.Fatalf("body = %q, want %q", ex.Request.Body, tc.want)
			}
		})
	}
}

func TestRun_HelperErrorsAreCatchable(t *testing.T) {
	t.Parallel()
	// A helper error becomes a JS exception the script can catch.
	script := `
		try {
			crypto.hmac("md5", "k", "d");
			request.body = "not reached";
		} catch (e) {
			request.body = "caught";
		}
	`
	ex := &Exchange{}
	if _, err := run(t, script, ex); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ex.Request.Body != "caught" {
		t.Fatalf("body = %q, want caught", ex.Request.Body)
	}
}

func TestRun_ScriptErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		script string
	}{
		{"syntax error", `this is not js`},
		{"thrown error", `throw new Error("boom");`},
		{"reference error", `undefinedFunc();`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := run(t, tc.script, &Exchange{}); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestRun_NilExchange(t *testing.T) {
	t.Parallel()
	if _, err := New().Run(context.Background(), `1+1`, nil); err == nil {
		t.Fatal("expected error for nil exchange")
	}
}

func TestRun_Timeout(t *testing.T) {
	t.Parallel()
	e := New(WithTimeout(50 * time.Millisecond))
	start := time.Now()
	_, err := e.Run(context.Background(), `while (true) {}`, &Exchange{})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout took too long: %s", elapsed)
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("err = %v, want timeout", err)
	}
}

func TestRun_ContextCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	// Long timeout so cancellation, not the timer, ends the loop.
	e := New(WithTimeout(10 * time.Second))
	if _, err := e.Run(ctx, `while (true) {}`, &Exchange{}); err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestRunChain_OrdersByPriorityAndThreadsState(t *testing.T) {
	t.Parallel()
	scripts := []Script{
		{Name: "third", Trigger: OnRequest, Priority: 30, Enabled: true, Body: `request.body += "C";`},
		{Name: "first", Trigger: OnRequest, Priority: 10, Enabled: true, Body: `request.body += "A";`},
		{Name: "second", Trigger: OnRequest, Priority: 20, Enabled: true, Body: `request.body += "B";`},
		{Name: "disabled", Trigger: OnRequest, Priority: 5, Enabled: false, Body: `request.body += "X";`},
		{Name: "wrong-trigger", Trigger: OnResponse, Priority: 1, Enabled: true, Body: `request.body += "Y";`},
	}
	ex := &Exchange{Request: Request{Body: "start:"}}
	chain := New().RunChain(context.Background(), OnRequest, scripts, ex)
	if ex.Request.Body != "start:ABC" {
		t.Fatalf("body = %q, want start:ABC", ex.Request.Body)
	}
	if len(chain.Results) != 3 {
		t.Fatalf("ran %d scripts, want 3", len(chain.Results))
	}
	gotNames := []string{chain.Results[0].Name, chain.Results[1].Name, chain.Results[2].Name}
	wantNames := []string{"first", "second", "third"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("order = %v, want %v", gotNames, wantNames)
	}
}

func TestRunChain_RejectionShortCircuits(t *testing.T) {
	t.Parallel()
	scripts := []Script{
		{Name: "a", Trigger: OnWebhook, Priority: 1, Enabled: true, Body: `request.body += "A";`},
		{Name: "gate", Trigger: OnWebhook, Priority: 2, Enabled: true, Body: `reject("blocked");`},
		{Name: "c", Trigger: OnWebhook, Priority: 3, Enabled: true, Body: `request.body += "C";`},
	}
	ex := &Exchange{Request: Request{Body: ""}}
	chain := New().RunChain(context.Background(), OnWebhook, scripts, ex)
	if !chain.Rejected || chain.RejectReason != "blocked" {
		t.Fatalf("chain = %+v, want rejected/blocked", chain)
	}
	if len(chain.Results) != 2 {
		t.Fatalf("ran %d scripts, want 2 (short-circuit)", len(chain.Results))
	}
	if ex.Request.Body != "A" {
		t.Fatalf("body = %q, want A (C should not run)", ex.Request.Body)
	}
}

func TestRunChain_ErrorDoesNotStopChain(t *testing.T) {
	t.Parallel()
	scripts := []Script{
		{Name: "ok1", Trigger: OnRequest, Priority: 1, Enabled: true, Body: `request.body += "A";`},
		{Name: "bad", Trigger: OnRequest, Priority: 2, Enabled: true, Body: `throw new Error("oops");`},
		{Name: "ok2", Trigger: OnRequest, Priority: 3, Enabled: true, Body: `request.body += "C";`},
	}
	ex := &Exchange{Request: Request{Body: ""}}
	chain := New().RunChain(context.Background(), OnRequest, scripts, ex)
	if len(chain.Results) != 3 {
		t.Fatalf("ran %d scripts, want 3", len(chain.Results))
	}
	if chain.Results[1].Err == nil {
		t.Fatal("expected error recorded for bad script")
	}
	if ex.Request.Body != "AC" {
		t.Fatalf("body = %q, want AC (chain continues past error)", ex.Request.Body)
	}
}

func TestWithTimeout_IgnoresNonPositive(t *testing.T) {
	t.Parallel()
	e := New(WithTimeout(-1))
	if e.timeout != 5*time.Second {
		t.Fatalf("timeout = %s, want default 5s", e.timeout)
	}
}

func TestFlattenHeader(t *testing.T) {
	t.Parallel()
	h := http.Header{
		"X-Single": {"one"},
		"X-Multi":  {"a", "b", "c"},
		"X-Empty":  {},
	}
	got := flattenHeader(h)
	want := map[string]string{"X-Single": "one", "X-Multi": "a, b, c", "X-Empty": ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("flattenHeader = %v, want %v", got, want)
	}
}

func TestExpandHeader(t *testing.T) {
	t.Parallel()
	m := map[string]string{"content-type": "application/json"}
	h := expandHeader(m)
	// http.Header.Set canonicalises the key.
	if got := h.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestHeaderRoundTrip(t *testing.T) {
	t.Parallel()
	orig := http.Header{}
	orig.Set("Authorization", "Bearer abc")
	orig.Set("X-Request-Id", "123")
	got := expandHeader(flattenHeader(orig))
	if !reflect.DeepEqual(got, orig) {
		t.Fatalf("round trip = %v, want %v", got, orig)
	}
}

// Guard against helper error wrapping regressions.
func TestBase64Decode_ErrorSurfaces(t *testing.T) {
	t.Parallel()
	_, err := run(t, `base64.decode("!!!not base64!!!");`, &Exchange{})
	if err == nil {
		t.Fatal("expected error from invalid base64")
	}
	if !errors.Is(err, err) { // sanity: err is a real error value
		t.Fatal("nil error unexpectedly")
	}
}
