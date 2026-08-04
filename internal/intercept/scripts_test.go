package intercept

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/intercept/proxy"
	"github.com/plutack/wiretap/internal/scripting"
	"github.com/plutack/wiretap/internal/store"
)

// newScriptStore returns a PCStore backed by a fresh in-memory SQLite DB with
// the given scripts inserted.
func newScriptStore(t *testing.T, scripts ...store.ScriptRow) *store.PCStore {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenInMemory("scripts-" + t.Name())
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.MigratePC(ctx, db); err != nil {
		t.Fatalf("MigratePC: %v", err)
	}
	st := store.NewPCStore(db)
	now := time.Unix(1_700_000_000, 0).UTC()
	for _, sc := range scripts {
		if _, err := st.InsertScript(ctx, sc, now); err != nil {
			t.Fatalf("InsertScript %q: %v", sc.Name, err)
		}
	}
	return st
}

func TestScriptTransformer_Nil(t *testing.T) {
	t.Parallel()
	// nil engine or nil store yields a nil transformer (identity at the proxy).
	if newScriptTransformer(nil, newScriptStore(t), nil) != nil {
		t.Error("nil engine should yield nil transformer")
	}
	if newScriptTransformer(scripting.New(), nil, nil) != nil {
		t.Error("nil store should yield nil transformer")
	}
}

func TestScriptTransformer_TransformRequest(t *testing.T) {
	t.Parallel()
	st := newScriptStore(t, store.ScriptRow{
		Name:    "sign",
		Trigger: string(scripting.OnRequest),
		Body: `
			request.headers["X-Signature"] = crypto.hmac("sha256", "secret", request.body);
			request.body = request.body.toUpperCase();
		`,
		Priority: 1,
		Enabled:  true,
	})
	tr := newScriptTransformer(scripting.New(), st, nil)

	in := proxy.ReqEdit{Method: "POST", URL: "https://x/y", Headers: http.Header{}, Body: []byte("hello")}
	out, err := tr.TransformRequest(context.Background(), in)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	if string(out.Body) != "HELLO" {
		t.Errorf("body = %q, want HELLO", out.Body)
	}
	want := "88aab3ede8d3adf94d26ab90d3bafd4a2083070c3bcce9c014ee04a443847c0b"
	if got := out.Headers.Get("X-Signature"); got != want {
		t.Errorf("X-Signature = %q, want %q", got, want)
	}
}

func TestScriptTransformer_ResponseChainOrder(t *testing.T) {
	t.Parallel()
	st := newScriptStore(t,
		store.ScriptRow{Name: "b", Trigger: string(scripting.OnResponse), Priority: 20, Enabled: true, Body: `response.body += "B";`},
		store.ScriptRow{Name: "a", Trigger: string(scripting.OnResponse), Priority: 10, Enabled: true, Body: `response.body += "A";`},
	)
	tr := newScriptTransformer(scripting.New(), st, nil)

	in := proxy.RespEdit{Status: 200, Headers: http.Header{}, Body: []byte("start:")}
	out, err := tr.TransformResponse(context.Background(), in)
	if err != nil {
		t.Fatalf("TransformResponse: %v", err)
	}
	if string(out.Body) != "start:AB" {
		t.Errorf("body = %q, want start:AB", out.Body)
	}
}

func TestScriptTransformer_RejectReturnsError(t *testing.T) {
	t.Parallel()
	st := newScriptStore(t, store.ScriptRow{
		Name:    "gate",
		Trigger: string(scripting.OnRequest),
		Body:    `reject("blocked by policy");`,
		Enabled: true,
	})
	tr := newScriptTransformer(scripting.New(), st, nil)

	_, err := tr.TransformRequest(context.Background(), proxy.ReqEdit{Headers: http.Header{}})
	var rej *RejectedError
	if !errors.As(err, &rej) {
		t.Fatalf("err = %v, want *RejectedError", err)
	}
	if rej.Reason != "blocked by policy" || rej.Trigger != scripting.OnRequest {
		t.Errorf("RejectedError = %+v", rej)
	}
}

func TestScriptTransformer_ScriptErrorReportedNotFatal(t *testing.T) {
	t.Parallel()
	st := newScriptStore(t,
		store.ScriptRow{Name: "bad", Trigger: string(scripting.OnRequest), Priority: 1, Enabled: true, Body: `throw new Error("boom");`},
		store.ScriptRow{Name: "good", Trigger: string(scripting.OnRequest), Priority: 2, Enabled: true, Body: `request.headers["X-Ok"] = "1";`},
	)
	var reported []string
	tr := newScriptTransformer(scripting.New(), st, func(_ scripting.Trigger, name string, err error) {
		if err != nil {
			reported = append(reported, name)
		}
	})

	out, err := tr.TransformRequest(context.Background(), proxy.ReqEdit{Headers: http.Header{}})
	if err != nil {
		t.Fatalf("TransformRequest should not fail on a script error: %v", err)
	}
	// The good script still ran despite the bad one erroring.
	if out.Headers.Get("X-Ok") != "1" {
		t.Errorf("expected good script to run; headers = %v", out.Headers)
	}
	if len(reported) != 1 || reported[0] != "bad" {
		t.Errorf("reported errors = %v, want [bad]", reported)
	}
}

func TestScriptTransformer_DisabledScriptsIgnored(t *testing.T) {
	t.Parallel()
	st := newScriptStore(t, store.ScriptRow{
		Name:    "off",
		Trigger: string(scripting.OnRequest),
		Body:    `request.body = "should not run";`,
		Enabled: false,
	})
	tr := newScriptTransformer(scripting.New(), st, nil)

	in := proxy.ReqEdit{Headers: http.Header{}, Body: []byte("keep")}
	out, err := tr.TransformRequest(context.Background(), in)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	if string(out.Body) != "keep" {
		t.Errorf("body = %q, want keep (disabled script must not run)", out.Body)
	}
}

func TestScriptTransformer_NoScriptsIsIdentity(t *testing.T) {
	t.Parallel()
	st := newScriptStore(t) // empty
	tr := newScriptTransformer(scripting.New(), st, nil)

	in := proxy.ReqEdit{Method: "GET", URL: "https://x/y", Headers: http.Header{"A": {"b"}}, Body: []byte("body")}
	out, err := tr.TransformRequest(context.Background(), in)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	if out.Method != "GET" || string(out.Body) != "body" {
		t.Errorf("identity broken: %+v", out)
	}
}

// fakeLoader lets a test force a load error without a broken DB.
type fakeLoader struct {
	err error
}

func (f fakeLoader) load(context.Context, scripting.Trigger) ([]scripting.Script, error) {
	return nil, f.err
}

func TestScriptTransformer_LoadErrorDegradesToIdentity(t *testing.T) {
	t.Parallel()
	var reportedErr error
	tr := &scriptTransformer{
		engine:  scripting.New(),
		loader:  fakeLoader{err: errors.New("db down")},
		onError: func(_ scripting.Trigger, _ string, err error) { reportedErr = err },
	}
	in := proxy.ReqEdit{Headers: http.Header{}, Body: []byte("x")}
	out, err := tr.TransformRequest(context.Background(), in)
	if err != nil {
		t.Fatalf("a load error must not fail the exchange: %v", err)
	}
	if string(out.Body) != "x" {
		t.Errorf("body changed on load error: %q", out.Body)
	}
	if reportedErr == nil {
		t.Error("expected load error to be reported via onError")
	}
}
