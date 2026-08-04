package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/relayclient"
	"github.com/plutack/wiretap/internal/scripting"
	"github.com/plutack/wiretap/internal/store"
)

// seedScript inserts an enabled on_webhook script, failing the test on error.
func seedScript(t *testing.T, st *store.PCStore, name, body string, priority int) {
	t.Helper()
	if _, err := st.InsertScript(context.Background(), store.ScriptRow{
		Name:     name,
		Trigger:  string(scripting.OnWebhook),
		Body:     body,
		Priority: priority,
		Enabled:  true,
	}, time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("InsertScript %q: %v", name, err)
	}
}

func TestWebhookTransformer_NilWhenNoEngineOrStore(t *testing.T) {
	t.Parallel()
	if newWebhookTransformer(nil, nil, nil) != nil {
		t.Error("nil engine + nil store must produce a nil transformer")
	}
	if newWebhookTransformer(scripting.New(), nil, nil) != nil {
		t.Error("nil store must produce a nil transformer")
	}
}

func TestWebhookTransformer_RewritesRow(t *testing.T) {
	t.Parallel()
	a, _ := openTestApp(t)
	ctx := context.Background()
	seedScript(t, a.store, "tag", `request.body = request.body + "!"; request.headers["X-Seen"] = "1";`, 0)

	wt := newWebhookTransformer(scripting.New(), a.store, nil)
	if wt == nil {
		t.Fatal("expected non-nil transformer")
	}
	row := store.WebhookRow{Method: "POST", Path: "/hook", HeadersJSON: "{}", Body: []byte("hi")}
	out, err := wt.TransformWebhook(ctx, row)
	if err != nil {
		t.Fatalf("TransformWebhook: %v", err)
	}
	if string(out.Body) != "hi!" {
		t.Errorf("Body = %q, want %q", out.Body, "hi!")
	}
	var hdrs http.Header
	if err := json.Unmarshal([]byte(out.HeadersJSON), &hdrs); err != nil {
		t.Fatalf("HeadersJSON not valid: %v", err)
	}
	if hdrs.Get("X-Seen") != "1" {
		t.Errorf("HeadersJSON = %q, want X-Seen: 1", out.HeadersJSON)
	}
}

func TestWebhookTransformer_RejectMapsToErrWebhookRejected(t *testing.T) {
	t.Parallel()
	a, _ := openTestApp(t)
	seedScript(t, a.store, "block", `reject("nope");`, 0)

	wt := newWebhookTransformer(scripting.New(), a.store, nil)
	_, err := wt.TransformWebhook(context.Background(), store.WebhookRow{Method: "POST", HeadersJSON: "{}"})
	if !errors.Is(err, relayclient.ErrWebhookRejected) {
		t.Errorf("err = %v, want ErrWebhookRejected", err)
	}
}

func TestWebhookTransformer_NoScriptsIsIdentity(t *testing.T) {
	t.Parallel()
	a, _ := openTestApp(t)
	wt := newWebhookTransformer(scripting.New(), a.store, nil)
	row := store.WebhookRow{Method: "POST", Path: "/x", HeadersJSON: "{}", Body: []byte("body")}
	out, err := wt.TransformWebhook(context.Background(), row)
	if err != nil {
		t.Fatalf("TransformWebhook: %v", err)
	}
	if string(out.Body) != "body" || out.Path != "/x" {
		t.Errorf("row changed with no scripts: %+v", out)
	}
}
