package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/plutack/wiretap/internal/api"
)

func TestBindings_RelayAdminOverview(t *testing.T) {
	t.Parallel()
	b, _ := newBindings(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(api.HealthResponse{Status: "ok", Version: "v9", TunnelCount: 2})
	})
	mux.HandleFunc("GET /admin/clients", func(w http.ResponseWriter, r *http.Request) {
		assertAdminToken(t, r)
		_ = json.NewEncoder(w).Encode(api.ListClientsResponse{Clients: []api.Client{{
			ClientID: "client-a", DisplayName: "workstation", Projects: []string{"orders"},
		}}})
	})
	mux.HandleFunc("GET /admin/projects", func(w http.ResponseWriter, r *http.Request) {
		assertAdminToken(t, r)
		_ = json.NewEncoder(w).Encode(api.ListProjectsResponse{Projects: []api.Project{{
			Path: "orders", ClientID: "client-a", AckedSeq: 17,
		}}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	view, err := b.RelayAdminOverview(RelayAdminInput{RelayURL: srv.URL, AdminToken: "admin-secret"})
	if err != nil {
		t.Fatalf("RelayAdminOverview: %v", err)
	}
	if view.BaseURL != srv.URL || view.Status != "ok" || view.Version != "v9" || view.TunnelCount != 2 {
		t.Fatalf("overview metadata = %+v", view)
	}
	if len(view.Clients) != 1 || view.Clients[0].ClientID != "client-a" {
		t.Fatalf("clients = %+v", view.Clients)
	}
	if len(view.Projects) != 1 || view.Projects[0].AckedSeq != 17 {
		t.Fatalf("projects = %+v", view.Projects)
	}
}

func TestBindings_RelayAdminCreateClientDoesNotReplaceDesktop(t *testing.T) {
	t.Parallel()
	b, a := newBindings(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		assertAdminToken(t, r)
		var req api.RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode register: %v", err)
		}
		if req.DisplayName != "build runner" || len(req.Projects) != 2 || req.Projects[0] != "events" {
			t.Fatalf("register request = %+v", req)
		}
		_ = json.NewEncoder(w).Encode(api.RegisterResponse{
			ClientID: "new-client", ClientToken: "one-time-token", Projects: req.Projects,
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	view, err := b.RelayAdminCreateClient(RelayAdminCreateClientInput{
		RelayURL: srv.URL, AdminToken: "admin-secret", DisplayName: " build runner ",
		Projects: []string{" /events/ ", "events", "audit"},
	})
	if err != nil {
		t.Fatalf("RelayAdminCreateClient: %v", err)
	}
	if view.ClientID != "new-client" || view.ClientToken != "one-time-token" || len(view.Projects) != 2 {
		t.Fatalf("credentials = %+v", view)
	}
	if _, err := a.RelayCredentials(); err == nil {
		t.Fatal("admin-created credentials unexpectedly replaced this desktop's identity")
	}
}

func TestBindings_RelayAdminMutations(t *testing.T) {
	t.Parallel()
	b, _ := newBindings(t)
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /admin/clients/client-a", func(w http.ResponseWriter, r *http.Request) {
		assertAdminToken(t, r)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /admin/projects", func(w http.ResponseWriter, r *http.Request) {
		assertAdminToken(t, r)
		var req api.ReclaimProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode reclaim: %v", err)
		}
		if req.Path != "orders" || req.NewClientID != "client-b" || !req.Force {
			t.Fatalf("reclaim request = %+v", req)
		}
		_ = json.NewEncoder(w).Encode(api.Project{Path: req.Path, ClientID: req.NewClientID})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	if err := b.RelayAdminDeleteClient(RelayAdminDeleteClientInput{
		RelayURL: srv.URL, AdminToken: "admin-secret", ClientID: "client-a",
	}); err != nil {
		t.Fatalf("RelayAdminDeleteClient: %v", err)
	}
	project, err := b.RelayAdminReassignProject(RelayAdminReassignProjectInput{
		RelayURL: srv.URL, AdminToken: "admin-secret", Path: "/orders/",
		NewClientID: "client-b", Force: true,
	})
	if err != nil {
		t.Fatalf("RelayAdminReassignProject: %v", err)
	}
	if project.Path != "orders" || project.ClientID != "client-b" {
		t.Fatalf("project = %+v", project)
	}
}

func TestRelayAdminClientValidation(t *testing.T) {
	t.Parallel()
	if _, _, err := relayAdminClient("https://relay.example.com", ""); err == nil {
		t.Fatal("missing token accepted")
	}
	if _, _, err := relayAdminClient("ftp://relay.example.com", "token"); err == nil {
		t.Fatal("unsupported URL accepted")
	}
}

func assertAdminToken(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("X-Admin-Token"); got != "admin-secret" {
		t.Fatalf("X-Admin-Token = %q", got)
	}
}
