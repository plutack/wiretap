package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/plutack/wiretap/internal/api"
)

// withTestRelayClient overrides the newRelayClient seam so commands talk to
// an httptest.Server running h. Registers t.Cleanup to restore the original.
// Returns the test server so callers can inspect received requests if needed.
func withTestRelayClient(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	orig := newRelayClient
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		newRelayClient = orig
		srv.Close()
	})
	newRelayClient = func(_ *cobra.Command) (*api.HTTPClient, error) {
		return api.NewClient(srv.URL, api.WithAdminToken("test-admin"))
	}
	return srv
}

func TestRelayCmd_Register(t *testing.T) {
	var gotBody api.RegisterRequest
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(api.RegisterResponse{
			ClientID: "c1", ClientToken: "t1", Projects: gotBody.Projects,
		})
	})
	withTestRelayClient(t, h)
	out, _, err := runCmd(t, "dev", "relay", "register",
		"--admin-token", "test-admin", "--projects", "alpha,beta", "--name", "lappy")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if len(gotBody.Projects) != 2 || gotBody.Projects[0] != "alpha" {
		t.Errorf("request projects = %v", gotBody.Projects)
	}
	if !strings.Contains(out, "c1") || !strings.Contains(out, "t1") {
		t.Errorf("stdout = %q, want client_id and token", out)
	}
}

func TestRelayCmd_ClientsList(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(api.ListClientsResponse{
			Clients: []api.Client{{ClientID: "c1", DisplayName: "lappy"}},
		})
	})
	withTestRelayClient(t, h)
	out, _, err := runCmd(t, "dev", "relay", "clients", "list", "--admin-token", "x")
	if err != nil {
		t.Fatalf("clients list: %v", err)
	}
	if !strings.Contains(out, "c1") {
		t.Errorf("stdout = %q, want c1", out)
	}
}

func TestRelayCmd_ClientsGet(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/admin/clients/ghost") {
			writeJSONTest(t, w, http.StatusNotFound, api.ErrorResponse{Code: "not_found", Message: "no such"})
			return
		}
		_ = json.NewEncoder(w).Encode(api.Client{ClientID: "c1"})
	})
	withTestRelayClient(t, h)
	out, _, err := runCmd(t, "dev", "relay", "clients", "get", "c1", "--admin-token", "x")
	if err != nil {
		t.Fatalf("clients get: %v", err)
	}
	if !strings.Contains(out, "c1") {
		t.Errorf("stdout = %q, want c1", out)
	}
	_, _, err = runCmd(t, "dev", "relay", "clients", "get", "ghost", "--admin-token", "x")
	if err == nil {
		t.Error("get ghost: expected error, got nil")
	}
}

func TestRelayCmd_ClientsDelete(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	withTestRelayClient(t, h)
	out, _, err := runCmd(t, "dev", "relay", "clients", "delete", "c1", "--admin-token", "x")
	if err != nil {
		t.Fatalf("clients delete: %v", err)
	}
	if !strings.Contains(out, "deleted") {
		t.Errorf("stdout = %q, want 'deleted'", out)
	}
}

func TestRelayCmd_ProjectsList(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(api.ListProjectsResponse{
			Projects: []api.Project{{Path: "alpha", ClientID: "c1", AckedSeq: 5}},
		})
	})
	withTestRelayClient(t, h)
	out, _, err := runCmd(t, "dev", "relay", "projects", "list", "--admin-token", "x")
	if err != nil {
		t.Fatalf("projects list: %v", err)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "5") {
		t.Errorf("stdout = %q, want alpha + acked_seq 5", out)
	}
}

func TestRelayCmd_ProjectsReclaim(t *testing.T) {
	var gotReq api.ReclaimProjectRequest
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		_ = json.NewEncoder(w).Encode(api.Project{Path: gotReq.Path, ClientID: gotReq.NewClientID})
	})
	withTestRelayClient(t, h)
	out, _, err := runCmd(t, "dev", "relay", "projects", "reclaim", "alpha",
		"--new-client", "c2", "--force", "--admin-token", "x")
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if gotReq.Path != "alpha" || gotReq.NewClientID != "c2" || !gotReq.Force {
		t.Errorf("request = %+v", gotReq)
	}
	if !strings.Contains(out, "c2") {
		t.Errorf("stdout = %q, want c2", out)
	}
}

func TestRelayCmd_WebhooksList(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(api.ListWebhooksResponse{
			Webhooks: []api.Webhook{{Project: "alpha", Seq: 1, Method: "POST"}},
		})
	})
	withTestRelayClient(t, h)
	out, _, err := runCmd(t, "dev", "relay", "webhooks", "list", "alpha",
		"--admin-token", "x", "--limit", "10")
	if err != nil {
		t.Fatalf("webhooks list: %v", err)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "POST") {
		t.Errorf("stdout = %q, want alpha + POST", out)
	}
}

func TestRelayCmd_WebhooksReplay(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	withTestRelayClient(t, h)
	out, _, err := runCmd(t, "dev", "relay", "webhooks", "replay", "alpha", "42",
		"--admin-token", "x")
	if err != nil {
		t.Fatalf("webhooks replay: %v", err)
	}
	if !strings.Contains(out, "replayed") {
		t.Errorf("stdout = %q, want 'replayed'", out)
	}
}

// writeJSONTest writes a JSON error response for test handlers.
func writeJSONTest(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
