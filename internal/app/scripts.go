package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/plutack/wiretap/internal/scripting"
	"github.com/plutack/wiretap/internal/store"
)

// ErrScriptEngineUnavailable is returned by TestScript when the app was built
// without a scripting engine (WithScriptEngine not passed). The CRUD methods do
// not need the engine — only test-runs evaluate JS.
var ErrScriptEngineUnavailable = errors.New("app: scripting engine not configured")

// errStoreNotOpen mirrors the inline message the read methods use; the script
// methods share it so "call Open first" reads the same everywhere.
var errStoreNotOpen = errors.New("app: store not open")

// Scripts lists every stored script (all triggers), ordered for the GUI
// sidebar. Returns an error before Open.
func (a *App) Scripts(ctx context.Context) ([]store.ScriptRow, error) {
	if a.store == nil {
		return nil, errStoreNotOpen
	}
	return a.store.Scripts(ctx)
}

// ScriptSummaries lists editor metadata without reading stored script bodies.
func (a *App) ScriptSummaries(ctx context.Context) ([]store.ScriptRow, error) {
	if a.store == nil {
		return nil, errStoreNotOpen
	}
	return a.store.ScriptSummaries(ctx)
}

// ScriptByID returns a single script. Wraps store.ErrNotFound when absent.
func (a *App) ScriptByID(ctx context.Context, id int64) (*store.ScriptRow, error) {
	if a.store == nil {
		return nil, errStoreNotOpen
	}
	return a.store.ScriptByID(ctx, id)
}

// CreateScript inserts a new script and returns its id. The trigger must be one
// of the four known triggers; the body is stored verbatim (not evaluated here).
func (a *App) CreateScript(ctx context.Context, sc store.ScriptRow) (int64, error) {
	if a.store == nil {
		return 0, errStoreNotOpen
	}
	if !scripting.Trigger(sc.Trigger).Valid() {
		return 0, fmt.Errorf("app: invalid trigger %q", sc.Trigger)
	}
	return a.store.InsertScript(ctx, sc, a.clock.Now())
}

// UpdateScript overwrites the mutable fields of the script identified by sc.ID.
// Wraps store.ErrNotFound when the id is unknown.
func (a *App) UpdateScript(ctx context.Context, sc store.ScriptRow) error {
	if a.store == nil {
		return errStoreNotOpen
	}
	if !scripting.Trigger(sc.Trigger).Valid() {
		return fmt.Errorf("app: invalid trigger %q", sc.Trigger)
	}
	return a.store.UpdateScript(ctx, sc, a.clock.Now())
}

// SetScriptEnabled toggles the enabled flag on one script.
func (a *App) SetScriptEnabled(ctx context.Context, id int64, enabled bool) error {
	if a.store == nil {
		return errStoreNotOpen
	}
	return a.store.SetScriptEnabled(ctx, id, enabled, a.clock.Now())
}

// DeleteScript removes a script by id.
func (a *App) DeleteScript(ctx context.Context, id int64) error {
	if a.store == nil {
		return errStoreNotOpen
	}
	return a.store.DeleteScript(ctx, id)
}

// ScriptTestInput is the sample exchange a test-run evaluates against. The GUI
// fills it from the currently selected capture/webhook (or defaults). All
// fields are optional; a nil engine returns ErrScriptEngineUnavailable.
type ScriptTestInput struct {
	Method  string
	URL     string
	Headers http.Header
	Body    string
	Status  int
}

// ScriptTestResult is the outcome of a single test-run: the (possibly mutated)
// request/response halves plus console logs and rejection state, so the editor
// can show what the script would do without touching live traffic.
type ScriptTestResult struct {
	Method       string
	URL          string
	ReqHeaders   http.Header
	ReqBody      string
	Status       int
	RespHeaders  http.Header
	RespBody     string
	Logs         []string
	Rejected     bool
	RejectReason string
}

// TestScript runs a single script body once against the given input and returns
// the mutated exchange + logs. It never persists anything and never touches
// live traffic — it is the "test-run" button behind the editor. A script
// exception is returned as an error (with any logs still attached in the
// result's caller path); a reject() is reported via Rejected/RejectReason.
func (a *App) TestScript(ctx context.Context, body string, in ScriptTestInput) (ScriptTestResult, error) {
	if a.scriptEngine == nil {
		return ScriptTestResult{}, ErrScriptEngineUnavailable
	}
	ex := &scripting.Exchange{}
	ex.SetRequest(in.Method, in.URL, in.Headers, []byte(in.Body))
	ex.SetResponse(in.Status, nil, nil)

	res, err := a.scriptEngine.Run(ctx, body, ex)
	method, url, reqHeaders, reqBody := ex.RequestParts()
	status, respHeaders, respBody := ex.ResponseParts()
	out := ScriptTestResult{
		Method:       method,
		URL:          url,
		ReqHeaders:   reqHeaders,
		ReqBody:      string(reqBody),
		Status:       status,
		RespHeaders:  respHeaders,
		RespBody:     string(respBody),
		Logs:         res.Logs,
		Rejected:     res.Rejected,
		RejectReason: res.RejectReason,
	}
	if err != nil {
		return out, err
	}
	return out, nil
}
