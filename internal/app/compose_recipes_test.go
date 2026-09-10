package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/plutack/wiretap/internal/scripting"
	"github.com/plutack/wiretap/internal/store"
)

const testComposeRecipe = `
const source = json.parse(request.body);
Object.keys(request.headers).forEach(function (key) { delete request.headers[key]; });
Object.keys(source.headers).forEach(function (key) { request.headers[key] = String(source.headers[key]); });
request.method = "PUT";
request.url = request.url.replace(/\/+$/, "") + "/webhook/test";
request.body = json.stringify(source.payload);
console.log("prepared " + source.payload.id);
`

func TestApp_ComposeRecipesListsOnlyEnabledComposeScripts(t *testing.T) {
	t.Parallel()
	a := openTestAppWithEngine(t)
	ctx := context.Background()
	for _, row := range []store.ScriptRow{
		{Name: "active recipe", Trigger: string(scripting.OnCompose), Body: testComposeRecipe, Enabled: true},
		{Name: "disabled recipe", Trigger: string(scripting.OnCompose), Body: testComposeRecipe, Enabled: false},
		{Name: "replay transform", Trigger: string(scripting.OnReplay), Body: "", Enabled: true},
	} {
		if _, err := a.CreateScript(ctx, row); err != nil {
			t.Fatalf("CreateScript(%s): %v", row.Name, err)
		}
	}
	recipes, err := a.ComposeRecipes(ctx)
	if err != nil {
		t.Fatalf("ComposeRecipes: %v", err)
	}
	if len(recipes) != 1 || recipes[0].Name != "active recipe" {
		t.Fatalf("ComposeRecipes = %+v", recipes)
	}
}

func TestApp_ApplyComposeRecipeBuildsEditableDraft(t *testing.T) {
	t.Parallel()
	a := openTestAppWithEngine(t)
	id, err := a.CreateScript(context.Background(), store.ScriptRow{
		Name: "adapter", Trigger: string(scripting.OnCompose), Body: testComposeRecipe, Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateScript: %v", err)
	}
	draft, err := a.ApplyComposeRecipe(
		context.Background(), "script:"+formatID(id),
		`{"headers":{"x-source":"yes"},"payload":{"id":"evt-17"}}`,
		"http://localhost:1700/",
	)
	if err != nil {
		t.Fatalf("ApplyComposeRecipe: %v", err)
	}
	if draft.Method != "PUT" || draft.URL != "http://localhost:1700/webhook/test" {
		t.Errorf("draft method/url = %s %s", draft.Method, draft.URL)
	}
	if draft.Headers.Get("X-Source") != "yes" || draft.Headers.Get("Content-Type") != "" {
		t.Errorf("draft headers = %v", draft.Headers)
	}
	if draft.Body != `{"id":"evt-17"}` {
		t.Errorf("draft body = %q", draft.Body)
	}
	if len(draft.Logs) != 1 || draft.Logs[0] != "prepared evt-17" {
		t.Errorf("draft logs = %v", draft.Logs)
	}
}

func TestApp_ApplyComposeRecipeRejectsSource(t *testing.T) {
	t.Parallel()
	a := openTestAppWithEngine(t)
	id, err := a.CreateScript(context.Background(), store.ScriptRow{
		Name: "validator", Trigger: string(scripting.OnCompose),
		Body: `if (request.body !== "ok") reject("wrong shape");`, Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateScript: %v", err)
	}
	_, err = a.ApplyComposeRecipe(context.Background(), "script:"+formatID(id), "bad", "http://localhost:1700")
	var rejected *ComposeRecipeRejectedError
	if !errors.As(err, &rejected) || rejected.Reason != "wrong shape" {
		t.Fatalf("err = %v, want ComposeRecipeRejectedError", err)
	}
}

func TestDocumentedComposeRecipeBuildsWebhook(t *testing.T) {
	t.Parallel()
	body, err := os.ReadFile("../../docs/recipes/source-record-to-webhook.js")
	if err != nil {
		t.Fatalf("read documented recipe: %v", err)
	}
	a := openTestAppWithEngine(t)
	id, err := a.CreateScript(context.Background(), store.ScriptRow{
		Name: "source record", Trigger: string(scripting.OnCompose), Body: string(body), Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateScript: %v", err)
	}
	draft, err := a.ApplyComposeRecipe(context.Background(), "script:"+formatID(id), `{"path":"/hooks/orders","headers":{"X-Event-Source":"sandbox"},"payload":{"event":"order.created","id":"evt_123"}}`, "http://localhost:1700/")
	if err != nil {
		t.Fatalf("ApplyComposeRecipe: %v", err)
	}
	if draft.Method != "POST" || draft.URL != "http://localhost:1700/hooks/orders" || draft.Body != `{"event":"order.created","id":"evt_123"}` {
		t.Errorf("draft = %+v", draft)
	}
	if draft.Headers.Get("X-Event-Source") != "sandbox" || draft.Headers.Get("Content-Type") != "application/json" {
		t.Errorf("headers = %v", draft.Headers)
	}
}

func formatID(id int64) string { return fmt.Sprintf("%d", id) }
