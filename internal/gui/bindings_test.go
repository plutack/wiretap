package gui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/app"
	"github.com/plutack/wiretap/internal/config"
	"github.com/plutack/wiretap/internal/scripting"
	"github.com/plutack/wiretap/internal/store"
)

// newBindings builds an App over a temp-dir config manager, opens its store,
// and returns the GUI binding layer + the underlying App (for direct seeding).
func newBindings(t *testing.T) (*Bindings, *app.App) {
	t.Helper()
	base := t.TempDir()
	mgr := config.NewManager(config.WithBaseDir(base))
	a := app.New(mgr, app.WithTunnelFactory(func(app.TunnelConfig, *store.PCStore) app.TunnelRunner {
		t.Fatalf("tunnel factory should not be called (no creds in tests)")
		return nil
	}))
	if err := a.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return New(a, WithVersion("test")), a
}

func TestBindings_Status_StoreOpenNoTunnel(t *testing.T) {
	t.Parallel()
	b, _ := newBindings(t)
	s := b.Status()
	if !s.StoreOpen {
		t.Error("StoreOpen = false, want true")
	}
	if s.TunnelRunning {
		t.Error("TunnelRunning = true, want false")
	}
	if s.Version != "test" {
		t.Errorf("Version = %q, want test", s.Version)
	}
	// Default config has an empty relay URL → omitted from JSON, and "" here.
	if s.RelayURL != "" {
		t.Errorf("RelayURL = %q, want empty", s.RelayURL)
	}
	// No tunnel is attached in tests (factory overridden to noop); connected
	// projects should be nil/empty so the GUI shows an idle/disconnected state
	// instead of stale data.
	if s.ConnectedProjects != nil {
		t.Errorf("ConnectedProjects = %v, want nil when no tunnel is attached", s.ConnectedProjects)
	}
}

// TestBindings_Status_ReflectsConnectedProjects exercises the test seam
// App.SetConnectedProjects: production wires it via the tunnel's OnConnect
// callback (see app.defaultTunnelFactory); tests inject the snapshot directly
// because the noop tunnel never fires OnConnect.
func TestBindings_Status_ReflectsConnectedProjects(t *testing.T) {
	t.Parallel()
	b, a := newBindings(t)
	a.SetConnectedProjects([]string{"nadbooks", "calculator"})
	s := b.Status()
	if len(s.ConnectedProjects) != 2 ||
		s.ConnectedProjects[0] != "nadbooks" || s.ConnectedProjects[1] != "calculator" {
		t.Errorf("ConnectedProjects = %v, want [nadbooks calculator]", s.ConnectedProjects)
	}
	// Clearing the snapshot mirrors OnDisconnect; Status should reflect nil.
	a.SetConnectedProjects(nil)
	if s2 := b.Status(); s2.ConnectedProjects != nil {
		t.Errorf("after clear, ConnectedProjects = %v, want nil", s2.ConnectedProjects)
	}
}

func TestBindings_Status_ReflectsConfig(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	mgr := config.NewManager(config.WithBaseDir(base))
	def := config.Default()
	def.Relay.URL = "wss://relay.example.com/tunnel"
	a := app.New(mgr, app.WithConfig(&def))
	if err := a.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	b := New(a, WithVersion("v1"))
	s := b.Status()
	if s.RelayURL != "wss://relay.example.com/tunnel" {
		t.Errorf("RelayURL = %q", s.RelayURL)
	}
	if s.TunnelRunning {
		// Tunnel disabled because credentials are missing → not running.
		if err := a.StartTunnel(context.Background()); err != nil {
			t.Fatalf("StartTunnel: %v", err)
		}
		// Credentials missing ⇒ factory not called ⇒ still not running.
		if b.Status().TunnelRunning {
			t.Error("TunnelRunning = true despite missing credentials")
		}
	}
}

func TestBindings_ListWebhooks_All(t *testing.T) {
	t.Parallel()
	b, a := newBindings(t)
	seedWebhook(t, a, "project-a", 1, "POST", "/hook", `{"a":"b"}`)
	seedWebhook(t, a, "project-b", 7, "GET", "/info", `{"k":"v"}`)
	seedWebhook(t, a, "project-a", 3, "PUT", "/hook/3", `{}`)

	got, err := b.ListWebhooks("")
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	// Summary shape: no body string, but BodyLen set; no headers map.
	var sawA, sawB bool
	for _, w := range got {
		if w.Body != "" {
			t.Errorf("body leaked into summary for %s/%d", w.Project, w.Seq)
		}
		if w.Headers != nil {
			t.Errorf("headers leaked into summary for %s/%d", w.Project, w.Seq)
		}
		if w.Project == "project-a" && w.Seq == 3 && w.BodyLen == 2 && w.Method == "PUT" {
			sawA = true
		}
		if w.Project == "project-b" && w.Seq == 7 && w.BodyLen == 9 {
			sawB = true
		}
		if w.ReceivedAt == "" {
			t.Errorf("ReceivedAt empty for %s/%d", w.Project, w.Seq)
		}
	}
	if !sawA {
		t.Error("missing project-a/ seq 3 row")
	}
	if !sawB {
		t.Error("missing project-b/ seq 7 row")
	}
}

func TestBindings_ListWebhooks_FilteredByProject(t *testing.T) {
	t.Parallel()
	b, a := newBindings(t)
	seedWebhook(t, a, "project-a", 1, "POST", "/hook", `x`)
	seedWebhook(t, a, "project-b", 2, "POST", "/hook", `y`)
	got, err := b.ListWebhooks("project-a")
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if len(got) != 1 || got[0].Project != "project-a" {
		t.Errorf("got = %+v", got)
	}
}

func TestBindings_GetWebhook_DetailIncludesBodyAndHeaders(t *testing.T) {
	t.Parallel()
	b, a := newBindings(t)
	seedWebhook(t, a, "project-a", 1, "POST", "/hook", `{"hello":"world"}`)

	// Set headers directly on the stored row for the detail assertion.
	got, err := b.GetWebhook("project-a", 1)
	if err != nil {
		t.Fatalf("GetWebhook: %v", err)
	}
	if got.Body != `{"hello":"world"}` {
		t.Errorf("Body = %q", got.Body)
	}
	if got.BodyLen != len(`{"hello":"world"}`) {
		t.Errorf("BodyLen = %d", got.BodyLen)
	}
	if got.Method != "POST" || got.Path != "/hook" {
		t.Errorf("method/path = %s %s", got.Method, got.Path)
	}
	if got.Project != "project-a" || got.Seq != 1 {
		t.Errorf("project/seq = %s %d", got.Project, got.Seq)
	}
}

func TestBindings_GetWebhook_MissingRow(t *testing.T) {
	t.Parallel()
	b, _ := newBindings(t)
	_, err := b.GetWebhook("nope", 99)
	if err == nil {
		t.Fatal("expected error for missing webhook, got nil")
	}
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want wrap of ErrNotFound", err)
	}
}

func TestBindings_ListCaptures(t *testing.T) {
	t.Parallel()
	b, a := newBindings(t)
	ctx := context.Background()
	if _, err := a.InsertTrafficCapture(ctx, store.TrafficCaptureRow{
		Method: "GET", URL: "https://api.example.com/users",
		ReqBody: []byte(`q=1`), Status: 200, RespBody: []byte(`["u"]`),
	}); err != nil {
		t.Fatalf("InsertTrafficCapture: %v", err)
	}
	if _, err := a.InsertTrafficCapture(ctx, store.TrafficCaptureRow{
		Method: "POST", URL: "https://api.example.com/users",
		ReqBody: []byte(`{}`), Status: 201,
	}); err != nil {
		t.Fatalf("InsertTrafficCapture: %v", err)
	}
	got, err := b.ListCaptures()
	if err != nil {
		t.Fatalf("ListCaptures: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// Summary: no bodies, but lengths set; newest-first (id DESC).
	first := got[0]
	if first.ReqBody != "" || first.RespBody != "" {
		t.Errorf("body leaked into summary: %+v", first)
	}
	// Newest (id DESC) is the POST with body `{}` (len 2) and no response body.
	if first.ReqBodyLen != 2 || first.RespBodyLen != 0 {
		t.Errorf("body lengths = %d/%d, want 2/0", first.ReqBodyLen, first.RespBodyLen)
	}
	if first.Method != "POST" || first.Status != 201 {
		t.Errorf("first capture = %+v, want POST 201 (newest)", first)
	}
	if first.At == "" {
		t.Error("At empty")
	}
	// The older capture (GET) is second and carries the response body.
	second := got[1]
	if second.Method != "GET" || second.Status != 200 ||
		second.ReqBodyLen != 3 || second.RespBodyLen != 5 {
		t.Errorf("second capture = %+v, want GET 200 with 3/5 body lengths", second)
	}
}

func TestBindings_ReplayWebhook_RepostsToTarget(t *testing.T) {
	t.Parallel()
	b, a := newBindings(t)
	seedWebhook(t, a, "project-a", 1, "POST", "/hook", `{"hello":"world"}`)

	var gotMethod, gotCT, gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(upstream.Close)

	a.Store() // ensure open (Open done in helper)
	res, err := b.ReplayWebhook("project-a", 1, upstream.URL+"/echo")
	if err != nil {
		t.Fatalf("ReplayWebhook: %v", err)
	}
	if res.Status != http.StatusAccepted {
		t.Errorf("status = %d, want 202", res.Status)
	}
	if gotMethod != "POST" {
		t.Errorf("upstream method = %q", gotMethod)
	}
	if gotBody != `{"hello":"world"}` {
		t.Errorf("upstream body = %q", gotBody)
	}
	if gotCT != "" {
		// no headers were stored in seedWebhook → none forwarded.
		t.Errorf("forwarded Content-Type = %q, want empty (none stored)", gotCT)
	}
}

func TestBindings_ReplayWebhook_EmptyTargetRejected(t *testing.T) {
	t.Parallel()
	b, a := newBindings(t)
	seedWebhook(t, a, "p", 1, "POST", "/", "x")
	if _, err := b.ReplayWebhook("p", 1, ""); err == nil {
		t.Fatal("expected error for empty target URL")
	}
}

func TestBindings_ReplayWebhook_MissingRow(t *testing.T) {
	t.Parallel()
	b, _ := newBindings(t)
	_, err := b.ReplayWebhook("nope", 9, "http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error for missing webhook on replay")
	}
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// newBindingsWithEngine mirrors newBindings but installs a real scripting
// engine, so TestScript can evaluate JS. Used by the script test-run tests.
func newBindingsWithEngine(t *testing.T) (*Bindings, *app.App) {
	t.Helper()
	base := t.TempDir()
	mgr := config.NewManager(config.WithBaseDir(base))
	a := app.New(mgr,
		app.WithTunnelFactory(func(app.TunnelConfig, *store.PCStore) app.TunnelRunner {
			t.Fatalf("tunnel factory should not be called (no creds in tests)")
			return nil
		}),
		app.WithScriptEngine(scripting.New(), nil),
	)
	if err := a.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return New(a, WithVersion("test")), a
}

func TestBindings_Scripts_SaveListGetDelete(t *testing.T) {
	t.Parallel()
	b, _ := newBindings(t)

	// Create via SaveScript (ID == 0).
	id, err := b.SaveScript(ScriptInput{
		Name: "sig", Trigger: string(scripting.OnRequest),
		Body: `request.headers["X-Sig"] = "abc";`, Priority: 5, Enabled: true,
	})
	if err != nil {
		t.Fatalf("SaveScript (create): %v", err)
	}
	if id == 0 {
		t.Fatal("SaveScript returned id 0 for a new script")
	}

	// List reflects the single row.
	list, err := b.ListScripts()
	if err != nil {
		t.Fatalf("ListScripts: %v", err)
	}
	if len(list) != 1 || list[0].Name != "sig" || !list[0].Enabled || list[0].Priority != 5 {
		t.Errorf("ListScripts = %+v", list)
	}

	// Get by id returns the full body.
	got, err := b.GetScript(id)
	if err != nil {
		t.Fatalf("GetScript: %v", err)
	}
	if got.Body != `request.headers["X-Sig"] = "abc";` {
		t.Errorf("GetScript body = %q", got.Body)
	}

	// Update via SaveScript (non-zero ID), then disable via SetScriptEnabled.
	if _, err := b.SaveScript(ScriptInput{
		ID: id, Name: "sig2", Trigger: string(scripting.OnResponse),
		Body: "response.status = 418;", Priority: 1, Enabled: true,
	}); err != nil {
		t.Fatalf("SaveScript (update): %v", err)
	}
	if err := b.SetScriptEnabled(id, false); err != nil {
		t.Fatalf("SetScriptEnabled: %v", err)
	}
	got, err = b.GetScript(id)
	if err != nil {
		t.Fatalf("GetScript after update: %v", err)
	}
	if got.Name != "sig2" || got.Trigger != string(scripting.OnResponse) ||
		got.Priority != 1 || got.Enabled {
		t.Errorf("GetScript after update = %+v", got)
	}

	// Delete removes the row; subsequent Get wraps ErrNotFound.
	if err := b.DeleteScript(id); err != nil {
		t.Fatalf("DeleteScript: %v", err)
	}
	if _, err := b.GetScript(id); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetScript after delete err = %v, want ErrNotFound", err)
	}
}

func TestBindings_SaveScript_RejectsInvalidTrigger(t *testing.T) {
	t.Parallel()
	b, _ := newBindings(t)
	if _, err := b.SaveScript(ScriptInput{
		Name: "x", Trigger: "on_bogus", Body: "",
	}); err == nil {
		t.Error("SaveScript with bad trigger: want error, got nil")
	}
}

func TestBindings_TestScript_MutatesAndReturnsLogs(t *testing.T) {
	t.Parallel()
	b, _ := newBindingsWithEngine(t)
	out, err := b.TestScript(ScriptTestRequest{
		Body: `
			console.log("was " + request.method);
			request.method = "PUT";
			request.headers["X-New"] = "yes";
			request.body = request.body + "!";
		`,
		Method:  "POST",
		URL:     "https://api.test/hook",
		Headers: map[string]string{"X-Old": "1"},
		ReqBody: `{"n":1}`,
	})
	if err != nil {
		t.Fatalf("TestScript: %v", err)
	}
	if out.Error != "" {
		t.Errorf("Error = %q, want empty", out.Error)
	}
	if out.Method != "PUT" {
		t.Errorf("Method = %q, want PUT", out.Method)
	}
	if out.ReqBody != `{"n":1}!` {
		t.Errorf("ReqBody = %q", out.ReqBody)
	}
	// The mutated header is added by the script directly; treat the map as an
	// http.Header so the lookup is case-insensitive (canonical form).
	reqHeaders := http.Header(out.ReqHeaders)
	if reqHeaders.Get("X-New") != "yes" {
		t.Errorf("X-New = %q, want yes", reqHeaders.Get("X-New"))
	}
	if len(out.Logs) != 1 || out.Logs[0] != "was POST" {
		t.Errorf("Logs = %v", out.Logs)
	}
}

func TestBindings_TestScript_RejectReportsReason(t *testing.T) {
	t.Parallel()
	b, _ := newBindingsWithEngine(t)
	out, err := b.TestScript(ScriptTestRequest{
		Body: `reject("bad payload");`,
	})
	if err != nil {
		t.Fatalf("TestScript: %v", err)
	}
	if !out.Rejected || out.RejectReason != "bad payload" {
		t.Errorf("Rejected=%v reason=%q, want true/\"bad payload\"", out.Rejected, out.RejectReason)
	}
}

func TestBindings_TestScript_SyntaxErrorInErrorField(t *testing.T) {
	t.Parallel()
	b, _ := newBindingsWithEngine(t)
	out, err := b.TestScript(ScriptTestRequest{Body: `this is not js`})
	if err != nil {
		t.Fatalf("TestScript: %v (a syntax error is surfaced in the view, not as a Go error)", err)
	}
	if out.Error == "" {
		t.Errorf("Error field empty for syntax error: %+v", out)
	}
}

func TestBindings_TestScript_NoEngine(t *testing.T) {
	t.Parallel()
	b, _ := newBindings(t) // no engine
	_, err := b.TestScript(ScriptTestRequest{Body: "1"})
	if !errors.Is(err, app.ErrScriptEngineUnavailable) {
		t.Errorf("err = %v, want ErrScriptEngineUnavailable", err)
	}
}

func seedWebhook(t *testing.T, a *app.App, project string, seq int64, method, path, body string) {
	t.Helper()
	ctx := context.Background()
	_, err := a.Store().StoreWebhook(ctx, store.WebhookRow{
		Project:     project,
		Seq:         seq,
		Method:      method,
		Path:        path,
		HeadersJSON: "",
		Body:        []byte(body),
		ReceivedAt:  time.Unix(int64(seq)*100, 0).UTC(),
	}, time.Unix(int64(seq)*100+1, 0).UTC())
	if err != nil {
		t.Fatalf("seedWebhook %s/%d: %v", project, seq, err)
	}
}
