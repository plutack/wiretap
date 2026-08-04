package intercept

import (
	"context"
	"fmt"

	"github.com/plutack/wiretap/internal/intercept/proxy"
	"github.com/plutack/wiretap/internal/scripting"
	"github.com/plutack/wiretap/internal/store"
)

// RejectedError is returned by the transformer when an on_request/on_response
// script calls reject(reason). The proxy treats a transform error as fatal to
// the exchange, so a rejection drops the connection (the client sees a 502 on
// the plain-HTTP path or a closed tunnel on the HTTPS path).
type RejectedError struct {
	Trigger scripting.Trigger
	Reason  string
}

func (e *RejectedError) Error() string {
	return fmt.Sprintf("intercept: %s script rejected the exchange: %s", e.Trigger, e.Reason)
}

// scriptEngine is the subset of *scripting.Engine the transformer needs,
// defined here (consumer side) so tests can inject a fake without a real goja
// runtime.
type scriptEngine interface {
	RunChain(ctx context.Context, trigger scripting.Trigger, scripts []scripting.Script, ex *scripting.Exchange) scripting.ChainResult
}

// scriptLoader fetches the enabled scripts for a trigger. Production is a thin
// adapter over *store.PCStore; tests supply a canned list.
type scriptLoader interface {
	load(ctx context.Context, trigger scripting.Trigger) ([]scripting.Script, error)
}

// storeScriptLoader adapts *store.PCStore to scriptLoader, mapping ScriptRow to
// scripting.Script. It loads only enabled scripts (the chain runner would skip
// disabled ones anyway; filtering in SQL keeps the payload small).
type storeScriptLoader struct{ st *store.PCStore }

func (l storeScriptLoader) load(ctx context.Context, trigger scripting.Trigger) ([]scripting.Script, error) {
	rows, err := l.st.ScriptsByTrigger(ctx, string(trigger), true)
	if err != nil {
		return nil, err
	}
	out := make([]scripting.Script, len(rows))
	for i, r := range rows {
		out[i] = scripting.Script{
			Name:     r.Name,
			Trigger:  scripting.Trigger(r.Trigger),
			Body:     r.Body,
			Priority: r.Priority,
			Enabled:  r.Enabled,
		}
	}
	return out, nil
}

// scriptTransformer adapts the scripting engine to proxy.Transformer, running
// on_request scripts before the request goes upstream and on_response scripts
// before the response returns to the client. It is the seam that makes user
// JavaScript take effect on live intercepted traffic.
//
// Failure policy: a script that throws is recorded in the chain result but does
// not abort the exchange (RunChain never stops on error), so one broken script
// never wedges the proxy. A script that calls reject(reason) aborts the
// exchange by returning an error, which the proxy surfaces as a dropped
// connection / 502. Load errors are logged via onError and treated as
// "no scripts" so a transient store hiccup doesn't break traffic.
type scriptTransformer struct {
	engine  scriptEngine
	loader  scriptLoader
	onError func(trigger scripting.Trigger, name string, err error)
}

// newScriptTransformer builds a transformer over a PCStore-backed engine. It
// returns nil when engine or store is nil so callers can pass the result
// straight to proxy.WithTransformer (a nil Transformer means "identity").
func newScriptTransformer(engine scriptEngine, st *store.PCStore, onError func(scripting.Trigger, string, error)) *scriptTransformer {
	if engine == nil || st == nil {
		return nil
	}
	return &scriptTransformer{
		engine:  engine,
		loader:  storeScriptLoader{st: st},
		onError: onError,
	}
}

// TransformRequest implements proxy.Transformer.
func (t *scriptTransformer) TransformRequest(ctx context.Context, in proxy.ReqEdit) (proxy.ReqEdit, error) {
	scripts, err := t.loadScripts(ctx, scripting.OnRequest)
	if err != nil || len(scripts) == 0 {
		return in, nil
	}
	ex := &scripting.Exchange{}
	ex.SetRequest(in.Method, in.URL, in.Headers, in.Body)
	chain := t.engine.RunChain(ctx, scripting.OnRequest, scripts, ex)
	t.reportErrors(scripting.OnRequest, chain)
	if chain.Rejected {
		return in, &RejectedError{Trigger: scripting.OnRequest, Reason: chain.RejectReason}
	}
	method, url, headers, body := ex.RequestParts()
	return proxy.ReqEdit{Method: method, URL: url, Headers: headers, Body: body}, nil
}

// TransformResponse implements proxy.Transformer.
func (t *scriptTransformer) TransformResponse(ctx context.Context, in proxy.RespEdit) (proxy.RespEdit, error) {
	scripts, err := t.loadScripts(ctx, scripting.OnResponse)
	if err != nil || len(scripts) == 0 {
		return in, nil
	}
	ex := &scripting.Exchange{}
	ex.SetResponse(in.Status, in.Headers, in.Body)
	chain := t.engine.RunChain(ctx, scripting.OnResponse, scripts, ex)
	t.reportErrors(scripting.OnResponse, chain)
	if chain.Rejected {
		return in, &RejectedError{Trigger: scripting.OnResponse, Reason: chain.RejectReason}
	}
	status, headers, body := ex.ResponseParts()
	return proxy.RespEdit{Status: status, Headers: headers, Body: body}, nil
}

// loadScripts fetches scripts for a trigger, reporting (but swallowing) load
// errors so a store hiccup degrades to "no scripts" rather than broken traffic.
func (t *scriptTransformer) loadScripts(ctx context.Context, trigger scripting.Trigger) ([]scripting.Script, error) {
	scripts, err := t.loader.load(ctx, trigger)
	if err != nil {
		if t.onError != nil {
			t.onError(trigger, "", err)
		}
		return nil, err
	}
	return scripts, nil
}

// reportErrors forwards each per-script error to onError so the GUI log pane
// (once wired) can surface them.
func (t *scriptTransformer) reportErrors(trigger scripting.Trigger, chain scripting.ChainResult) {
	if t.onError == nil {
		return
	}
	for _, r := range chain.Results {
		if r.Err != nil {
			t.onError(trigger, r.Name, r.Err)
		}
	}
}
