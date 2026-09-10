package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/plutack/wiretap/internal/scripting"
)

// ComposeRecipe is an enabled, user-authored source-to-request adapter.
// Recipes are stored as ordinary on_compose scripts and never run implicitly.
type ComposeRecipe struct {
	ID          string
	Name        string
	Description string
}

// ComposeRecipeDraft is the complete editable request produced by a recipe.
type ComposeRecipeDraft struct {
	Method  string
	URL     string
	Headers http.Header
	Body    string
	Logs    []string
}

// ComposeRecipes lists enabled user-authored on_compose scripts. Wiretap does
// not ship provider-specific recipes; users own and edit every adapter.
func (a *App) ComposeRecipes(ctx context.Context) ([]ComposeRecipe, error) {
	if a.store == nil {
		return nil, errStoreNotOpen
	}
	rows, err := a.store.ScriptsByTrigger(ctx, string(scripting.OnCompose), true)
	if err != nil {
		return nil, fmt.Errorf("app: list compose recipes: %w", err)
	}
	out := make([]ComposeRecipe, 0, len(rows))
	for _, row := range rows {
		out = append(out, ComposeRecipe{
			ID: "script:" + strconv.FormatInt(row.ID, 10), Name: row.Name,
			Description: "Local source-to-request recipe",
		})
	}
	return out, nil
}

// ApplyComposeRecipe runs one selected recipe against source text. It performs
// no network I/O and does not invoke the enabled on_replay chain; the result is
// returned to the composer for inspection and editing before Send is pressed.
func (a *App) ApplyComposeRecipe(ctx context.Context, recipeID, source, baseURL string) (ComposeRecipeDraft, error) {
	if a.scriptEngine == nil {
		return ComposeRecipeDraft{}, ErrScriptEngineUnavailable
	}
	baseURL = strings.TrimSpace(baseURL)
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ComposeRecipeDraft{}, fmt.Errorf("app: recipe base URL must be absolute http(s): %q", baseURL)
	}
	body, err := a.composeRecipeBody(ctx, recipeID)
	if err != nil {
		return ComposeRecipeDraft{}, err
	}
	result, runErr := a.TestScript(ctx, body, ScriptTestInput{
		Method: http.MethodPost, URL: baseURL,
		Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: source,
	})
	if runErr != nil {
		return ComposeRecipeDraft{}, fmt.Errorf("app: apply compose recipe: %w", runErr)
	}
	if result.Rejected {
		return ComposeRecipeDraft{}, &ComposeRecipeRejectedError{Reason: result.RejectReason}
	}
	parsed, err = url.Parse(strings.TrimSpace(result.URL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ComposeRecipeDraft{}, fmt.Errorf("app: recipe produced an invalid request URL %q", result.URL)
	}
	return ComposeRecipeDraft{
		Method: result.Method, URL: result.URL, Headers: result.ReqHeaders,
		Body: result.ReqBody, Logs: result.Logs,
	}, nil
}

func (a *App) composeRecipeBody(ctx context.Context, recipeID string) (string, error) {
	const prefix = "script:"
	if !strings.HasPrefix(recipeID, prefix) {
		return "", fmt.Errorf("app: unknown compose recipe %q", recipeID)
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(recipeID, prefix), 10, 64)
	if err != nil || id <= 0 {
		return "", fmt.Errorf("app: invalid compose recipe %q", recipeID)
	}
	row, err := a.ScriptByID(ctx, id)
	if err != nil {
		return "", fmt.Errorf("app: get compose recipe: %w", err)
	}
	if row.Trigger != string(scripting.OnCompose) || !row.Enabled {
		return "", errors.New("app: compose recipe is not enabled")
	}
	return row.Body, nil
}

// ComposeRecipeRejectedError reports a recipe's deliberate source validation
// failure separately from JavaScript/runtime errors.
type ComposeRecipeRejectedError struct{ Reason string }

func (e *ComposeRecipeRejectedError) Error() string {
	return "app: compose recipe rejected source: " + e.Reason
}
