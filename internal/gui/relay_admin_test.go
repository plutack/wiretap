package gui

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutack/wiretap/internal/api"
	"github.com/plutack/wiretap/internal/app"
	"github.com/plutack/wiretap/internal/config"
	"github.com/plutack/wiretap/internal/secretstore"
)

type memorySecrets struct {
	values map[string]string
	fail   error
	drop   bool
}

func (m *memorySecrets) Set(account, secret string) error {
	if m.fail != nil {
		return m.fail
	}
	if m.drop {
		return nil
	}
	m.values[account] = secret
	return nil
}
func (m *memorySecrets) Get(account string) (string, error) {
	value, ok := m.values[account]
	if !ok {
		return "", secretstore.ErrNotFound
	}
	return value, nil
}
func (m *memorySecrets) Delete(account string) error {
	if _, ok := m.values[account]; !ok {
		return secretstore.ErrNotFound
	}
	delete(m.values, account)
	return nil
}

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
	if view.Format != config.RelayClientFileFormat || view.Version != config.RelayClientFileVersion ||
		view.RelayURL != "ws"+strings.TrimPrefix(srv.URL, "http")+"/tunnel" {
		t.Fatalf("portable client file metadata = %+v", view)
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
	mux.HandleFunc("PUT /admin/projects/audit", func(w http.ResponseWriter, r *http.Request) {
		assertAdminToken(t, r)
		var req api.AssignProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ClientID != "client-b" {
			t.Fatalf("assign request = %+v, err = %v", req, err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(api.Project{Path: "audit", ClientID: req.ClientID})
	})
	mux.HandleFunc("DELETE /admin/projects/audit", func(w http.ResponseWriter, r *http.Request) {
		assertAdminToken(t, r)
		w.WriteHeader(http.StatusNoContent)
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
	created, err := b.RelayAdminAddProject(RelayAdminAddProjectInput{
		RelayURL: srv.URL, AdminToken: "admin-secret", Path: "/audit/", ClientID: "client-b",
	})
	if err != nil || created.Path != "audit" {
		t.Fatalf("RelayAdminAddProject = %+v, %v", created, err)
	}
	if err := b.RelayAdminDeleteProject(RelayAdminDeleteProjectInput{
		RelayURL: srv.URL, AdminToken: "admin-secret", Path: "/audit/",
	}); err != nil {
		t.Fatalf("RelayAdminDeleteProject: %v", err)
	}
}

func TestBindings_RelayAdminSubscriptionsAndWebhookDeletion(t *testing.T) {
	t.Parallel()
	b, _ := newBindings(t)
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /admin/projects/orders/subscribers/client-b", func(w http.ResponseWriter, r *http.Request) {
		assertAdminToken(t, r)
		if r.URL.Query().Get("include_history") != "true" {
			t.Error("include_history was not forwarded")
		}
		_ = json.NewEncoder(w).Encode(api.Project{Path: "orders", Subscriptions: []api.ProjectSubscription{{ClientID: "client-b"}}})
	})
	mux.HandleFunc("DELETE /admin/projects/orders/subscribers/client-b", func(w http.ResponseWriter, r *http.Request) {
		assertAdminToken(t, r)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /admin/projects/orders/webhooks", func(w http.ResponseWriter, r *http.Request) {
		assertAdminToken(t, r)
		_ = json.NewEncoder(w).Encode(api.ListWebhooksResponse{Webhooks: []api.Webhook{{
			Project: "orders", Seq: 7, ReceivedAt: 1700000000, Method: "POST", Path: "/paid", BodyBytes: 5,
		}}})
	})
	mux.HandleFunc("POST /admin/projects/orders/webhooks/batch-delete", func(w http.ResponseWriter, r *http.Request) {
		assertAdminToken(t, r)
		var req api.DeleteWebhooksRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.Seqs) != 1 || req.Seqs[0] != 7 {
			t.Fatalf("delete request = %+v", req)
		}
		_ = json.NewEncoder(w).Encode(api.DeleteWebhooksResponse{Deleted: 1})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	project, err := b.RelayAdminAddSubscriber(RelayAdminSubscriptionInput{
		RelayURL: srv.URL, AdminToken: "admin-secret", Path: "orders", ClientID: "client-b", IncludeHistory: true,
	})
	if err != nil || len(project.Subscriptions) != 1 {
		t.Fatalf("add subscriber = %+v, %v", project, err)
	}
	page, err := b.RelayAdminListWebhooks(RelayAdminListWebhooksInput{
		RelayURL: srv.URL, AdminToken: "admin-secret", Path: "orders", Limit: 50,
	})
	if err != nil || len(page.Webhooks) != 1 || page.Webhooks[0].BodyBytes != 5 {
		t.Fatalf("webhook page = %+v, %v", page, err)
	}
	deleted, err := b.RelayAdminDeleteWebhooks(RelayAdminDeleteWebhooksInput{
		RelayURL: srv.URL, AdminToken: "admin-secret", Path: "orders", Seqs: []int64{7},
	})
	if err != nil || deleted != 1 {
		t.Fatalf("deleted = %d, %v", deleted, err)
	}
	if err := b.RelayAdminRemoveSubscriber(RelayAdminSubscriptionInput{
		RelayURL: srv.URL, AdminToken: "admin-secret", Path: "orders", ClientID: "client-b",
	}); err != nil {
		t.Fatal(err)
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

func TestRelayAdminProfilesStoreTokenOutsideMetadata(t *testing.T) {
	t.Parallel()
	b, a := newBindings(t)
	secrets := &memorySecrets{values: map[string]string{}}
	b.secrets = secrets
	profile, err := b.RelayAdminSaveProfile(RelayAdminSaveProfileInput{
		RelayURL: "https://relay.example.com", AdminToken: "top-secret", Name: "Production",
	})
	if err != nil {
		t.Fatalf("RelayAdminSaveProfile: %v", err)
	}
	if profile.Name != "Production" || profile.ID == "" {
		t.Fatalf("profile = %+v", profile)
	}
	profiles, err := b.RelayAdminProfiles()
	if err != nil || len(profiles) != 1 {
		t.Fatalf("profiles = %+v, %v", profiles, err)
	}
	session, err := b.RelayAdminLoadProfile(profile.ID)
	if err != nil || session.AdminToken != "top-secret" || session.RelayURL != "https://relay.example.com" {
		t.Fatalf("session = %+v, %v", session, err)
	}
	dir, _ := a.ConfigDir()
	metadata, err := os.ReadFile(filepath.Join(dir, "relay-admin-profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(metadata), "top-secret") {
		t.Fatal("admin token leaked into profile metadata")
	}
	if err := b.RelayAdminDeleteProfile(profile.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := secrets.Get(relayAdminProfileAccount(profile.ID)); !errors.Is(err, secretstore.ErrNotFound) {
		t.Fatalf("secret remains: %v", err)
	}
}

func TestRelayAdminProfileKeyringFailureDoesNotWriteMetadata(t *testing.T) {
	t.Parallel()
	b, _ := newBindings(t)
	b.secrets = &memorySecrets{values: map[string]string{}, fail: errors.New("locked")}
	if _, err := b.RelayAdminSaveProfile(RelayAdminSaveProfileInput{RelayURL: "https://relay.example.com", AdminToken: "secret"}); err == nil {
		t.Fatal("keyring failure accepted")
	}
	profiles, err := b.RelayAdminProfiles()
	if err != nil || len(profiles) != 0 {
		t.Fatalf("profiles = %+v, %v", profiles, err)
	}
}

func TestRelayAdminProfileUnreadableWriteDoesNotWriteMetadata(t *testing.T) {
	t.Parallel()
	b, _ := newBindings(t)
	b.secrets = &memorySecrets{values: map[string]string{}, drop: true}
	if _, err := b.RelayAdminSaveProfile(RelayAdminSaveProfileInput{
		RelayURL: "https://relay.example.com", AdminToken: "secret",
	}); err == nil || !strings.Contains(err.Error(), "verify relay profile") {
		t.Fatalf("unreadable keyring write error = %v", err)
	}
	profiles, err := b.RelayAdminProfiles()
	if err != nil || len(profiles) != 0 {
		t.Fatalf("profiles = %+v, %v", profiles, err)
	}
}

func TestRelayAdminAddProjectSynchronizesCurrentDesktop(t *testing.T) {
	t.Parallel()
	b, a, starts := newSettingsBindings(t)
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /admin/projects/orders", func(w http.ResponseWriter, r *http.Request) {
		assertAdminToken(t, r)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(api.Project{Path: "orders", ClientID: "client-local"})
	})
	mux.HandleFunc("GET /admin/clients/client-local", func(w http.ResponseWriter, r *http.Request) {
		assertAdminToken(t, r)
		_ = json.NewEncoder(w).Encode(api.Client{ClientID: "client-local", Projects: []string{"orders", "audit"}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	tunnelURL, err := app.TunnelURLFromBase(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Relay.URL = tunnelURL
	if err := a.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveRelayCredentials(config.Credentials{ClientID: "client-local", ClientToken: "secret", Projects: []string{"audit"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RelayAdminAddProject(RelayAdminAddProjectInput{
		RelayURL: srv.URL, AdminToken: "admin-secret", Path: "orders", ClientID: "client-local",
	}); err != nil {
		t.Fatalf("RelayAdminAddProject: %v", err)
	}
	creds, err := a.RelayCredentials()
	if err != nil || len(creds.Projects) != 2 || creds.Projects[0] != "audit" || creds.Projects[1] != "orders" {
		t.Fatalf("credentials = %+v, %v", creds, err)
	}
	if starts.Load() != 1 {
		t.Fatalf("tunnel starts = %d, want 1", starts.Load())
	}
}

func assertAdminToken(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("X-Admin-Token"); got != "admin-secret" {
		t.Fatalf("X-Admin-Token = %q", got)
	}
}
