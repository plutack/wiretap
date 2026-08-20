package export

import (
	"strings"
	"testing"
)

func sampleRequest() Request {
	return Request{
		Method: "POST",
		URL:    "https://relay.example.com/project-a/orders/created?dry=1",
		Headers: map[string][]string{
			"Content-Type":        {"application/json"},
			"X-Webhook-Signature": {"abc123"},
			"Content-Length":      {"23"}, // must be dropped
			"Connection":          {"keep-alive"},
		},
		Body: []byte(`{"order_id":"test-123"}`),
	}
}

func TestSnippet_ShellCurl(t *testing.T) {
	t.Parallel()
	out, err := Snippet(sampleRequest(), "shell", "curl")
	if err != nil {
		t.Fatalf("Snippet: %v", err)
	}
	for _, want := range []string{
		"curl",
		"--request POST",
		"https://relay.example.com/project-a/orders/created?dry=1",
		"Content-Type: application/json",
		"X-Webhook-Signature: abc123",
		`{"order_id":"test-123"}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("snippet missing %q:\n%s", want, out)
		}
	}
	for _, banned := range []string{"Content-Length", "keep-alive"} {
		if strings.Contains(out, banned) {
			t.Errorf("snippet should not contain %q:\n%s", banned, out)
		}
	}
}

func TestSnippet_DefaultClient(t *testing.T) {
	t.Parallel()
	// Empty client key selects the target default (shell -> curl).
	out, err := Snippet(sampleRequest(), "shell", "")
	if err != nil {
		t.Fatalf("Snippet: %v", err)
	}
	if !strings.Contains(out, "curl") {
		t.Errorf("default shell client should be curl:\n%s", out)
	}
}

func TestSnippet_MoreTargets(t *testing.T) {
	t.Parallel()
	cases := []struct {
		target, client, want string
	}{
		{"javascript", "fetch", "fetch("},
		{"python", "requests", "import requests"},
		{"go", "native", "http.NewRequest"},
		{"node", "", "http"},
	}
	for _, tc := range cases {
		out, err := Snippet(sampleRequest(), tc.target, tc.client)
		if err != nil {
			t.Errorf("Snippet(%s/%s): %v", tc.target, tc.client, err)
			continue
		}
		if !strings.Contains(out, tc.want) {
			t.Errorf("Snippet(%s/%s) missing %q:\n%s", tc.target, tc.client, tc.want, out)
		}
	}
}

func TestSnippet_GetWithoutBody(t *testing.T) {
	t.Parallel()
	out, err := Snippet(Request{
		Method:  "GET",
		URL:     "https://api.example.com/v1/things",
		Headers: map[string][]string{"Accept": {"application/json"}},
	}, "shell", "curl")
	if err != nil {
		t.Fatalf("Snippet: %v", err)
	}
	if !strings.Contains(out, "--request GET") && !strings.Contains(out, "--url") {
		t.Errorf("unexpected GET snippet:\n%s", out)
	}
	if strings.Contains(out, "--data") {
		t.Errorf("GET snippet should have no body:\n%s", out)
	}
}

func TestSnippet_UnknownTarget(t *testing.T) {
	t.Parallel()
	if _, err := Snippet(sampleRequest(), "cobol", ""); err == nil {
		t.Fatal("expected error for unknown target")
	}
	if _, err := Snippet(sampleRequest(), "", ""); err == nil {
		t.Fatal("expected error for empty target")
	}
}

func TestTargets(t *testing.T) {
	t.Parallel()
	ts, err := Targets()
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if len(ts) < 10 {
		t.Fatalf("expected a rich target catalog, got %d", len(ts))
	}
	byKey := map[string]Target{}
	for _, target := range ts {
		byKey[target.Key] = target
	}
	shell, ok := byKey["shell"]
	if !ok {
		t.Fatal("missing shell target")
	}
	if shell.DefaultClient != "curl" {
		t.Errorf("shell default client = %q, want curl", shell.DefaultClient)
	}
	foundCurl := false
	for _, c := range shell.Clients {
		if c.Key == "curl" {
			foundCurl = true
		}
	}
	if !foundCurl {
		t.Error("shell target missing curl client")
	}
}

func TestSnippet_Concurrent(t *testing.T) {
	t.Parallel()
	// Fresh runtimes per call must make concurrent conversions safe.
	done := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			_, err := Snippet(sampleRequest(), "shell", "curl")
			done <- err
		}()
	}
	for i := 0; i < 8; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent Snippet: %v", err)
		}
	}
}
