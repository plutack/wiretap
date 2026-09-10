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

func TestDocumentedNuvionRecipeHandlesBridgeAndFuse(t *testing.T) {
	t.Parallel()
	body, err := os.ReadFile("../../docs/recipes/nuvion-heroku-webhook.js")
	if err != nil {
		t.Fatalf("read documented recipe: %v", err)
	}
	a := openTestAppWithEngine(t)
	id, err := a.CreateScript(context.Background(), store.ScriptRow{
		Name: "nuvion", Trigger: string(scripting.OnCompose), Body: string(body), Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateScript: %v", err)
	}
	tests := []struct {
		name, source, wantURL, wantBody string
	}{
		{"bridge", `{"message":{"data":{"body":{"event_category":"customer","event_id":"bridge-1"},"headers":{"host":"remote.example","content-type":"application/json","x-request-id":"req-1"}}}}`, "http://localhost:1700/webhook/bridge", `{"event_category":"customer","event_id":"bridge-1"}`},
		{"fuse", `{"message":{"data":{"body":{"type":"account_opened","event_id":"fuse-1"},"headers":{"host":"remote.example","content-type":"application/json","x-request-id":"req-2"}}}}`, "http://localhost:1700/webhook/fuse", `{"type":"account_opened","event_id":"fuse-1"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			draft, err := a.ApplyComposeRecipe(context.Background(), "script:"+formatID(id), tc.source, "http://localhost:1700")
			if err != nil {
				t.Fatalf("ApplyComposeRecipe: %v", err)
			}
			if draft.Method != "POST" || draft.URL != tc.wantURL || draft.Body != tc.wantBody {
				t.Errorf("draft = %+v", draft)
			}
			if draft.Headers.Get("Host") != "" || draft.Headers.Get("X-Request-ID") == "" || draft.Headers.Get("Content-Type") != "application/json" {
				t.Errorf("headers = %v", draft.Headers)
			}
		})
	}
}

func formatID(id int64) string { return fmt.Sprintf("%d", id) }
