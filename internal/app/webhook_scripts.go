package app

import (
	"context"
	"encoding/json"

	"github.com/plutack/wiretap/internal/relayclient"
	"github.com/plutack/wiretap/internal/scripting"
	"github.com/plutack/wiretap/internal/store"
)

// webhookTransformer adapts the scripting engine to relayclient.WebhookTransformer,
// running on_webhook scripts against an incoming webhook before relayclient
// persists it. It is the app-layer seam that keeps relayclient free of a
// scripting import (relayclient defines the interface; app supplies this).
//
// Failure policy mirrors the other triggers: a thrown script is recorded via
// onError but does not stop the chain, so one broken script never wedges
// ingestion. reject(reason) maps to relayclient.ErrWebhookRejected, which drops
// the row (still ACKed). A store load error degrades to "store as received".
type webhookTransformer struct {
	engine  *scripting.Engine
	store   *store.PCStore
	onError func(trigger scripting.Trigger, name string, err error)
}

// newWebhookTransformer returns an adapter, or nil when engine or store is nil
// so the caller can pass the result straight to relayclient.WithWebhookTransformer
// (a nil transformer means "store as received").
func newWebhookTransformer(e *scripting.Engine, st *store.PCStore, onError func(scripting.Trigger, string, error)) *webhookTransformer {
	if e == nil || st == nil {
		return nil
	}
	return &webhookTransformer{engine: e, store: st, onError: onError}
}

// TransformWebhook implements relayclient.WebhookTransformer. It exposes the
// webhook to scripts as the `request` global (method/path/headers/body); any
// mutations are written back onto the row before storage. Header edits update
// both HeadersJSON (the queryable form scripts see) — RawHeaders is left as
// received so faithful replay still reflects the original bytes.
func (t *webhookTransformer) TransformWebhook(ctx context.Context, row store.WebhookRow) (store.WebhookRow, error) {
	rows, err := t.store.ScriptsByTrigger(ctx, string(scripting.OnWebhook), true)
	if err != nil {
		if t.onError != nil {
			t.onError(scripting.OnWebhook, "", err)
		}
		return row, nil
	}
	if len(rows) == 0 {
		return row, nil
	}
	scripts := make([]scripting.Script, len(rows))
	for i, r := range rows {
		scripts[i] = scripting.Script{
			Name:     r.Name,
			Trigger:  scripting.Trigger(r.Trigger),
			Body:     r.Body,
			Priority: r.Priority,
			Enabled:  r.Enabled,
		}
	}

	ex := &scripting.Exchange{}
	ex.SetRequest(row.Method, row.Path, parseHeaders(row.HeadersJSON), row.Body)
	chain := t.engine.RunChain(ctx, scripting.OnWebhook, scripts, ex)
	if t.onError != nil {
		for _, r := range chain.Results {
			if r.Err != nil {
				t.onError(scripting.OnWebhook, r.Name, r.Err)
			}
		}
	}
	if chain.Rejected {
		return row, relayclient.ErrWebhookRejected
	}

	method, path, headers, body := ex.RequestParts()
	row.Method = method
	row.Path = path
	row.Body = body
	if encoded, err := json.Marshal(headers); err == nil {
		row.HeadersJSON = string(encoded)
	}
	return row, nil
}
