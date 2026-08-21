package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/plutack/wiretap/internal/export"
	"github.com/plutack/wiretap/internal/store"
)

// freshPCStore stands up an in-memory migrated PCStore for TUI tests.
func freshPCStore(t *testing.T) *store.PCStore {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenInMemory(fmt.Sprintf("tui-%s", t.Name()))
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.MigratePC(ctx, db); err != nil {
		t.Fatalf("MigratePC: %v", err)
	}
	return store.NewPCStore(db)
}

var tuiFixedTime = time.Unix(1_700_000_000, 0).UTC()

// storeDeps wires the read/script seams over an in-memory store — the same
// shape internal/cli/tui.go produces from *app.App. Replay/export/status are
// per-test fakes.
func storeDeps(st *store.PCStore) Deps {
	return Deps{
		Webhooks:          st.Webhooks,
		CapturesBySession: st.TrafficCapturesBySession,
		Sessions:          st.InterceptSessions,
		Scripts:           st.Scripts,
		SetScriptEnabled: func(ctx context.Context, id int64, enabled bool) error {
			return st.SetScriptEnabled(ctx, id, enabled, tuiFixedTime)
		},
	}
}

// keyPress builds the tea.KeyMsg Update would receive for a key string.
func keyPress(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// run drives the model through a sequence of messages, returning the final
// model and the batch of commands produced along the way (execed lazily).
func run(t *testing.T, m Model, msgs ...tea.Msg) (Model, []tea.Cmd) {
	t.Helper()
	var cmds []tea.Cmd
	cur := tea.Model(m)
	for _, msg := range msgs {
		next, cmd := cur.Update(msg)
		cur = next
		cmds = append(cmds, cmd)
	}
	return cur.(Model), cmds
}

func seedWebhook(t *testing.T, st *store.PCStore, project string, seq int64, method, path string, body []byte) {
	t.Helper()
	_, err := st.StoreWebhook(context.Background(), store.WebhookRow{
		Project: project, Seq: seq, ReceivedAt: tuiFixedTime, Method: method, Path: path,
		HeadersJSON: `{"Content-Type":["application/json"]}`, Body: body,
	}, tuiFixedTime)
	if err != nil {
		t.Fatalf("StoreWebhook: %v", err)
	}
}

func mustTick(t *testing.T, m Model) Model {
	t.Helper()
	out, _ := run(t, m, tickMsg{})
	return out
}

func TestRefresh_LoadsRows(t *testing.T) {
	st := freshPCStore(t)
	for i := 0; i < 3; i++ {
		seedWebhook(t, st, "project-a", int64(i+1), "POST", "/x", []byte("body"))
	}

	m := mustTick(t, New(storeDeps(st)))

	if len(m.webhookRows) != 3 {
		t.Fatalf("webhookRows = %d, want 3", len(m.webhookRows))
	}
	// Newest-first (Webhooks returns ORDER BY seq DESC).
	if m.webhookRows[0].Seq != 3 {
		t.Errorf("first row seq = %d, want 3 (newest-first)", m.webhookRows[0].Seq)
	}
	if got := len(m.ingress.Items()); got != 3 {
		t.Errorf("ingress items = %d, want 3", got)
	}
	if m.err != nil {
		t.Errorf("err = %v, want nil", m.err)
	}
}

func TestRefresh_LoadsTrafficAndScripts(t *testing.T) {
	st := freshPCStore(t)
	ctx := context.Background()
	if _, err := st.CreateInterceptSession(ctx, tuiFixedTime, "bash", "127.0.0.1:8888"); err != nil {
		t.Fatalf("CreateInterceptSession: %v", err)
	}
	if _, err := st.InsertTrafficCapture(ctx, store.TrafficCaptureRow{
		SessionID: 1, At: tuiFixedTime, Method: "GET", URL: "http://example.com/api",
		ReqHeadersJSON: "{}", ReqBody: []byte("{}"), Status: 200,
		RespHeadersJSON: "{}", RespBody: []byte(`{"ok":true}`),
	}); err != nil {
		t.Fatalf("InsertTrafficCapture: %v", err)
	}
	if _, err := st.InsertScript(ctx, store.ScriptRow{
		Name: "bump-timestamp", Trigger: "on_replay", Body: "// js", Enabled: true,
	}, tuiFixedTime); err != nil {
		t.Fatalf("InsertScript: %v", err)
	}

	m := mustTick(t, New(storeDeps(st)))

	if len(m.captureRows) != 1 || m.captureRows[0].Status != 200 {
		t.Errorf("captureRows = %+v, want one 200 capture", m.captureRows)
	}
	if len(m.sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(m.sessions))
	}
	if len(m.scriptRows) != 1 || len(m.scripts.Items()) != 1 {
		t.Errorf("scriptRows/items = %d/%d, want 1/1", len(m.scriptRows), len(m.scripts.Items()))
	}
}

func TestCursorStickyAcrossRefresh(t *testing.T) {
	st := freshPCStore(t)
	seedWebhook(t, st, "p", 1, "POST", "/one", nil)
	seedWebhook(t, st, "p", 2, "POST", "/two", nil)

	m := mustTick(t, New(storeDeps(st)))
	m.ingress.Select(1) // oldest row, p/1

	// A new arrival shifts every index down; the cursor must stay on p/1.
	seedWebhook(t, st, "p", 3, "POST", "/three", nil)
	m = mustTick(t, m)

	it, ok := m.ingress.SelectedItem().(webhookItem)
	if !ok {
		t.Fatalf("selected item type = %T", m.ingress.SelectedItem())
	}
	if it.row.Seq != 1 {
		t.Errorf("cursor followed the shift onto seq %d, want it pinned to seq 1", it.row.Seq)
	}
}

func TestPauseFreezesListAndCountsBacklog(t *testing.T) {
	st := freshPCStore(t)
	seedWebhook(t, st, "p", 1, "POST", "/one", nil)

	m := mustTick(t, New(storeDeps(st)))
	out, _ := run(t, m, keyPress("p"))
	if !out.paused {
		t.Fatal("p should pause the live feed")
	}

	seedWebhook(t, st, "p", 2, "POST", "/two", nil)
	out = mustTick(t, out)
	if got := len(out.ingress.Items()); got != 1 {
		t.Errorf("paused list items = %d, want 1 (frozen)", got)
	}
	if out.backlog != 1 {
		t.Errorf("backlog = %d, want 1", out.backlog)
	}

	out, _ = run(t, out, keyPress("p"))
	if out.paused || out.backlog != 0 {
		t.Errorf("after resume: paused=%v backlog=%d, want false/0", out.paused, out.backlog)
	}
	if got := len(out.ingress.Items()); got != 2 {
		t.Errorf("resumed list items = %d, want 2", got)
	}
}

func TestMethodLensFiltersWebhooks(t *testing.T) {
	st := freshPCStore(t)
	seedWebhook(t, st, "p", 1, "GET", "/a", nil)
	seedWebhook(t, st, "p", 2, "POST", "/b", nil)

	m := mustTick(t, New(storeDeps(st)))

	out, _ := run(t, m, keyPress("f")) // "" → GET
	if out.methodLens != "GET" {
		t.Fatalf("methodLens = %q, want GET", out.methodLens)
	}
	if got := len(out.ingress.Items()); got != 1 {
		t.Errorf("filtered items = %d, want 1 (GET only)", got)
	}

	out, _ = run(t, out, keyPress("c")) // clear
	if out.methodLens != "" || len(out.ingress.Items()) != 2 {
		t.Errorf("after clear: lens=%q items=%d, want \"\"/2", out.methodLens, len(out.ingress.Items()))
	}
}

func TestStatusLensFiltersCaptures(t *testing.T) {
	st := freshPCStore(t)
	ctx := context.Background()
	for i, status := range []int{200, 404, 500} {
		if _, err := st.InsertTrafficCapture(ctx, store.TrafficCaptureRow{
			ID: int64(i + 1), At: tuiFixedTime, Method: "GET", URL: "http://x", Status: status,
		}); err != nil {
			t.Fatalf("InsertTrafficCapture: %v", err)
		}
	}

	m := mustTick(t, New(storeDeps(st)))
	m.tab = tabTraffic
	m.applyRows()

	out, _ := run(t, m, keyPress("F"), keyPress("F")) // "" → 2xx → 3xx
	if out.statusLens != "3xx" {
		t.Fatalf("statusLens = %q, want 3xx", out.statusLens)
	}
	// No 3xx captures were seeded; the next tick keeps the list empty.
	out = mustTick(t, out)
	if got := len(out.traffic.Items()); got != 0 {
		t.Errorf("3xx items = %d, want 0", got)
	}
}

func TestSessionFilterDrivesQuery(t *testing.T) {
	st := freshPCStore(t)
	ctx := context.Background()
	s1, _ := st.CreateInterceptSession(ctx, tuiFixedTime, "bash", "127.0.0.1:8888")
	s2, _ := st.CreateInterceptSession(ctx, tuiFixedTime, "fish", "127.0.0.1:8888")
	for _, s := range []int64{s1, s2} {
		if _, err := st.InsertTrafficCapture(ctx, store.TrafficCaptureRow{
			SessionID: s, At: tuiFixedTime, Method: "GET", URL: "http://x", Status: 200,
		}); err != nil {
			t.Fatalf("InsertTrafficCapture: %v", err)
		}
	}

	var queried []int64
	deps := storeDeps(st)
	deps.CapturesBySession = func(ctx context.Context, sessionID int64, limit int) ([]store.TrafficCaptureRow, error) {
		queried = append(queried, sessionID)
		return st.TrafficCapturesBySession(ctx, sessionID, limit)
	}

	m := mustTick(t, New(deps))
	m.tab = tabTraffic

	out, _ := run(t, m, keyPress("S")) // all → newest session (s2, id 2)
	if got := len(out.captureRows); got != 1 {
		t.Fatalf("session-filtered captures = %d, want 1", got)
	}
	if out.captureRows[0].SessionID != s2 {
		t.Errorf("filtered session = %d, want %d", out.captureRows[0].SessionID, s2)
	}
}

func TestEnterOpensWebhookDetail(t *testing.T) {
	st := freshPCStore(t)
	seedWebhook(t, st, "proj", 7, "POST", "/stripe/hook", []byte(`{"a":1}`))

	m := mustTick(t, New(storeDeps(st)))
	out, _ := run(t, m, tea.WindowSizeMsg{Width: 100, Height: 30}, keyPress("enter"))

	if out.focus != focusDetail || out.detail == nil {
		t.Fatalf("focus = %v detail = %v, want focusDetail/non-nil", out.focus, out.detail)
	}
	view := out.View()
	for _, want := range []string{"POST proj/7", "/stripe/hook", "Headers"} {
		if !strings.Contains(view, want) {
			t.Errorf("detail view missing %q", want)
		}
	}

	// esc returns to the lists.
	out, _ = run(t, out, keyPress("esc"))
	if out.focus != focusLists || out.detail != nil {
		t.Errorf("esc: focus = %v detail = %v, want focusLists/nil", out.focus, out.detail)
	}
}

func TestCaptureDetailShowsBothHalves(t *testing.T) {
	st := freshPCStore(t)
	cap := store.TrafficCaptureRow{
		ID: 3, SessionID: 1, At: tuiFixedTime, Method: "GET", URL: "http://api.test/x",
		ReqHeadersJSON: `{"Accept":["application/json"]}`, ReqBody: []byte(`{"q":"hi"}`),
		Status:          201,
		RespHeadersJSON: `{"Content-Type":["application/json"]}`, RespBody: []byte(`{"ok":true}`),
	}
	if _, err := st.InsertTrafficCapture(context.Background(), cap); err != nil {
		t.Fatalf("InsertTrafficCapture: %v", err)
	}

	m := mustTick(t, New(storeDeps(st)))
	m.tab = tabTraffic
	m.applyRows()
	out, _ := run(t, m, tea.WindowSizeMsg{Width: 100, Height: 30}, keyPress("enter"))

	if out.detail == nil || out.detail.kind != detailCapture {
		t.Fatalf("detail = %+v, want capture detail", out.detail)
	}
	view := out.View()
	for _, want := range []string{"Request headers", "Response · 201", `"ok": true`} {
		if !strings.Contains(view, want) {
			t.Errorf("capture detail missing %q", want)
		}
	}

	// tab jumps to the response half.
	before := out.detail.vp.YOffset
	out, _ = run(t, out, keyPress("tab"))
	if out.detail.half != 1 || out.detail.vp.YOffset < before {
		t.Errorf("tab: half=%d yoff=%d (was %d), want half 1 scrolled down", out.detail.half, out.detail.vp.YOffset, before)
	}
}

func TestReplayFlow(t *testing.T) {
	st := freshPCStore(t)
	seedWebhook(t, st, "proj", 9, "POST", "/hook", []byte(`{}`))

	var gotProject string
	var gotSeq int64
	var gotTarget string
	deps := storeDeps(st)
	deps.Replay = func(_ context.Context, project string, seq int64, target string) (int, error) {
		gotProject, gotSeq, gotTarget = project, seq, target
		return 200, nil
	}
	deps.Status = func() StatusSnapshot {
		return StatusSnapshot{Version: "test", ForwardURL: "http://localhost:3000/wh"}
	}

	m := mustTick(t, New(deps))
	out, _ := run(t, m, keyPress("r"))
	if out.focus != focusReplay || out.replay == nil {
		t.Fatalf("r: focus = %v replay = %v, want focusReplay/non-nil", out.focus, out.replay)
	}
	if v := out.replay.input.Value(); v != "http://localhost:3000/wh" {
		t.Errorf("replay prefill = %q, want forward URL", v)
	}

	out, cmds := run(t, out, keyPress("enter"))
	if out.replay != nil {
		t.Error("enter should close the prompt")
	}
	var replayCmd tea.Cmd
	for _, c := range cmds {
		if c != nil {
			replayCmd = c
		}
	}
	if replayCmd == nil {
		t.Fatal("enter produced no replay command")
	}
	msg := replayCmd()
	res, ok := msg.(replayResultMsg)
	if !ok || res.err != nil {
		t.Fatalf("replay cmd produced %#v", msg)
	}
	if gotProject != "proj" || gotSeq != 9 || gotTarget != "http://localhost:3000/wh" {
		t.Errorf("replay called with (%q,%d,%q)", gotProject, gotSeq, gotTarget)
	}

	out, _ = run(t, out, res)
	if s, _ := out.toastText(); !strings.Contains(s, "HTTP 200") {
		t.Errorf("toast = %q, want replay success", s)
	}
}

func TestReplayEmptyTargetCancels(t *testing.T) {
	st := freshPCStore(t)
	seedWebhook(t, st, "proj", 1, "POST", "/hook", nil)

	m := mustTick(t, New(storeDeps(st)))
	out, _ := run(t, m, keyPress("r"))
	out.replay.input.SetValue("   ")

	out, cmds := run(t, out, keyPress("enter"))
	for _, c := range cmds {
		if c != nil {
			t.Fatal("blank target must not produce a replay command")
		}
	}
	if s, _ := out.toastText(); !strings.Contains(s, "no target") {
		t.Errorf("toast = %q, want cancellation notice", s)
	}
}

func TestExportFlowStickySelection(t *testing.T) {
	st := freshPCStore(t)
	seedWebhook(t, st, "proj", 4, "POST", "/hook", []byte(`{}`))

	var gotTarget, gotClient string
	deps := storeDeps(st)
	deps.ExportTargets = func() ([]export.Target, error) {
		return []export.Target{
			{Key: "shell", Title: "Shell", DefaultClient: "curl", Clients: []export.Client{{Key: "curl", Title: "cURL"}, {Key: "httpie", Title: "HTTPie"}}},
			{Key: "go", Title: "Go", DefaultClient: "net/http", Clients: []export.Client{{Key: "net/http", Title: "net/http"}}},
		}, nil
	}
	deps.ExportWebhook = func(_ context.Context, _ string, _ int64, target, client string) (string, error) {
		gotTarget, gotClient = target, client
		return "curl -X POST ...", nil
	}

	m := mustTick(t, New(deps))
	out, cmds := run(t, m, keyPress("e"))
	if out.focus != focusExport || out.export == nil {
		t.Fatalf("e: focus = %v export = %v", out.focus, out.export)
	}

	// Resolve the targets fetch that 'e' kicked off.
	var targets tea.Cmd
	for _, c := range cmds {
		if c != nil {
			targets = c
		}
	}
	if targets == nil {
		t.Fatal("'e' produced no fetch-targets command")
	}
	out, _ = run(t, out, targets())
	if out.export.stage != exportPickTarget {
		t.Fatalf("stage = %v, want pickTarget", out.export.stage)
	}

	// Pick target #2 (go), then its non-default client; the final enter
	// returns the snippet render command.
	out, cmds = run(t, out, keyPress("j"), keyPress("enter"), keyPress("j"), keyPress("enter"))
	if out.export.stage != exportShowSnippet {
		t.Fatalf("stage = %v, want showSnippet", out.export.stage)
	}
	snip := cmds[len(cmds)-1]
	if snip == nil {
		t.Fatal("final enter produced no snippet command")
	}
	out, _ = run(t, out, snip())
	if out.export.snippet != "curl -X POST ..." {
		t.Errorf("snippet = %q", out.export.snippet)
	}
	if gotTarget != "go" || gotClient != "net/http" {
		t.Errorf("exported with target=%q client=%q, want go/net/http", gotTarget, gotClient)
	}
	if out.lastTarget != "go" || out.lastClient != "net/http" {
		t.Errorf("sticky = %q/%q, want go/net/http", out.lastTarget, out.lastClient)
	}
}

func TestScriptToggle(t *testing.T) {
	st := freshPCStore(t)
	ctx := context.Background()
	id, err := st.InsertScript(ctx, store.ScriptRow{
		Name: "bump", Trigger: "on_replay", Body: "// js", Enabled: false,
	}, tuiFixedTime)
	if err != nil {
		t.Fatalf("InsertScript: %v", err)
	}

	m := mustTick(t, New(storeDeps(st)))
	out, _ := run(t, m, keyPress("3")) // transforms tab
	if out.tab != tabTransforms {
		t.Fatalf("tab = %v, want transforms", out.tab)
	}

	_, cmds := run(t, out, keyPress(" "))
	var toggle tea.Cmd
	for _, c := range cmds {
		if c != nil {
			toggle = c
		}
	}
	if toggle == nil {
		t.Fatal("space produced no toggle command")
	}
	msg := toggle()
	res, ok := msg.(scriptToggleMsg)
	if !ok || res.err != nil {
		t.Fatalf("toggle cmd produced %#v", msg)
	}
	if !res.enabled {
		t.Error("toggle should enable a disabled script")
	}

	out, _ = run(t, out, res)
	got, err := st.ScriptByID(ctx, id)
	if err != nil || got == nil || !got.Enabled {
		t.Errorf("stored script after toggle = %+v err=%v, want enabled", got, err)
	}
	if s, _ := out.toastText(); !strings.Contains(s, "enabled") {
		t.Errorf("toast = %q, want enable notice", s)
	}
}

func TestView_RendersSomething(t *testing.T) {
	st := freshPCStore(t)
	seedWebhook(t, st, "p", 1, "POST", "/x", []byte("b"))

	m := mustTick(t, New(storeDeps(st)))
	out, _ := run(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})

	view := out.View()
	if view == "" {
		t.Fatal("View returned empty string")
	}
	for _, want := range []string{"wiretap", "Ingress", "Traffic", "Transforms"} {
		if !strings.Contains(view, want) {
			t.Errorf("View should contain %q", want)
		}
	}
}

func TestHeaderLineShowsStatus(t *testing.T) {
	st := freshPCStore(t)
	deps := storeDeps(st)
	deps.Status = func() StatusSnapshot {
		return StatusSnapshot{
			Version: "1.2.3", RelayURL: "wss://relay.example.com/tunnel",
			TunnelRunning: true, ConnectedProjects: []string{"proj-a", "proj-b"},
		}
	}

	m := mustTick(t, New(deps))
	header := m.headerLine()
	for _, want := range []string{"v1.2.3", "relay.example.com", "connected", "proj-a"} {
		if !strings.Contains(header, want) {
			t.Errorf("header missing %q: %s", want, header)
		}
	}
}

func TestQuitKeys(t *testing.T) {
	st := freshPCStore(t)
	m := mustTick(t, New(storeDeps(st)))

	for _, k := range []string{"q", "ctrl+c"} {
		_, cmds := run(t, m, keyPress(k))
		quit := false
		for _, c := range cmds {
			if c != nil {
				if _, ok := c().(tea.QuitMsg); ok {
					quit = true
				}
			}
		}
		if !quit {
			t.Errorf("key %q should quit", k)
		}
	}
}

func TestThemeFor(t *testing.T) {
	if got := themeFor("light").name; got != "light" {
		t.Errorf("themeFor(light) = %q", got)
	}
	if got := themeFor("dark").name; got != "dark" {
		t.Errorf("themeFor(dark) = %q", got)
	}
	if got := themeFor("bogus").name; got != "dark" {
		t.Errorf("themeFor(bogus) = %q, want dark fallback", got)
	}
}
