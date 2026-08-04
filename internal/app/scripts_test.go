package app

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/plutack/wiretap/internal/config"
	"github.com/plutack/wiretap/internal/scripting"
	"github.com/plutack/wiretap/internal/store"
)

// openTestAppWithEngine is openTestApp plus a real scripting engine, for the
// TestScript path.
func openTestAppWithEngine(t *testing.T) *App {
	t.Helper()
	base := t.TempDir()
	mgr := config.NewManager(config.WithBaseDir(base))
	a := New(mgr, WithScriptEngine(scripting.New(), nil))
	if err := a.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func TestApp_ScriptCRUD(t *testing.T) {
	t.Parallel()
	a, _ := openTestApp(t)
	ctx := context.Background()

	id, err := a.CreateScript(ctx, store.ScriptRow{
		Name: "sig", Trigger: string(scripting.OnRequest), Body: "request.method = 'PUT';", Priority: 5, Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateScript: %v", err)
	}

	got, err := a.ScriptByID(ctx, id)
	if err != nil {
		t.Fatalf("ScriptByID: %v", err)
	}
	if got.Name != "sig" || got.Priority != 5 || !got.Enabled {
		t.Errorf("ScriptByID = %+v", got)
	}

	got.Body = "request.method = 'DELETE';"
	got.Priority = 1
	if err := a.UpdateScript(ctx, *got); err != nil {
		t.Fatalf("UpdateScript: %v", err)
	}
	if err := a.SetScriptEnabled(ctx, id, false); err != nil {
		t.Fatalf("SetScriptEnabled: %v", err)
	}

	list, err := a.Scripts(ctx)
	if err != nil {
		t.Fatalf("Scripts: %v", err)
	}
	if len(list) != 1 || list[0].Enabled || list[0].Priority != 1 {
		t.Errorf("Scripts = %+v", list)
	}

	if err := a.DeleteScript(ctx, id); err != nil {
		t.Fatalf("DeleteScript: %v", err)
	}
	if _, err := a.ScriptByID(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ScriptByID after delete err = %v, want ErrNotFound", err)
	}
}

func TestApp_CreateScript_RejectsInvalidTrigger(t *testing.T) {
	t.Parallel()
	a, _ := openTestApp(t)
	if _, err := a.CreateScript(context.Background(), store.ScriptRow{
		Name: "x", Trigger: "on_bogus", Body: "",
	}); err == nil {
		t.Error("CreateScript with bad trigger: want error, got nil")
	}
}

func TestApp_TestScript_MutatesAndLogs(t *testing.T) {
	t.Parallel()
	a := openTestAppWithEngine(t)
	in := ScriptTestInput{
		Method:  "POST",
		URL:     "https://api.test/hook",
		Headers: http.Header{"X-Old": {"1"}},
		Body:    `{"n":1}`,
	}
	out, err := a.TestScript(context.Background(), `
		console.log("before: " + request.body);
		request.method = "PUT";
		request.headers["X-New"] = "yes";
		request.body = request.body + "!";
	`, in)
	if err != nil {
		t.Fatalf("TestScript: %v", err)
	}
	if out.Method != "PUT" {
		t.Errorf("Method = %q, want PUT", out.Method)
	}
	if out.ReqBody != `{"n":1}!` {
		t.Errorf("ReqBody = %q", out.ReqBody)
	}
	if out.ReqHeaders.Get("X-New") != "yes" {
		t.Errorf("X-New header = %q, want yes", out.ReqHeaders.Get("X-New"))
	}
	if len(out.Logs) != 1 || out.Logs[0] != `before: {"n":1}` {
		t.Errorf("Logs = %v", out.Logs)
	}
}

func TestApp_TestScript_Reject(t *testing.T) {
	t.Parallel()
	a := openTestAppWithEngine(t)
	out, err := a.TestScript(context.Background(), `reject("bad payload");`, ScriptTestInput{Method: "POST"})
	if err != nil {
		t.Fatalf("TestScript: %v", err)
	}
	if !out.Rejected || out.RejectReason != "bad payload" {
		t.Errorf("Rejected=%v reason=%q, want true/'bad payload'", out.Rejected, out.RejectReason)
	}
}

func TestApp_TestScript_SyntaxErrorReturnsError(t *testing.T) {
	t.Parallel()
	a := openTestAppWithEngine(t)
	if _, err := a.TestScript(context.Background(), `this is not js`, ScriptTestInput{}); err == nil {
		t.Error("TestScript with syntax error: want error, got nil")
	}
}

func TestApp_TestScript_NoEngine(t *testing.T) {
	t.Parallel()
	a, _ := openTestApp(t) // no engine
	if _, err := a.TestScript(context.Background(), "1", ScriptTestInput{}); !errors.Is(err, ErrScriptEngineUnavailable) {
		t.Errorf("err = %v, want ErrScriptEngineUnavailable", err)
	}
}
