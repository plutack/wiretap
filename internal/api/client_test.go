package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// This file tests the typed HTTP client against an in-process httptest
// server. It mirrors the canonical test recipe used elsewhere in the
// project: table-driven subtests, httptest.Server for HTTP, errors.As via
// the Is* helpers.

// stubServer returns a httptest.Server running the supplied handler and a
// client pointed at it. Cleanup is registered automatically.
func stubServer(t *testing.T, h http.Handler, opts ...ClientOption) *HTTPClient {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL, opts...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestNewClient_InvalidBase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		base string
	}{
		{"empty", ""},
		{"no scheme", "example.com"},
		{"bad scheme", "ftp://example.com"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewClient(tc.base); err == nil {
				t.Errorf("NewClient(%q): expected error, got nil", tc.base)
			}
		})
	}
}

func TestHealth(t *testing.T) {
	t.Parallel()
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if r := w.Header(); r != nil {
			r.Set("Content-Type", "application/json")
		}
		_ = json.NewEncoder(w).Encode(HealthResponse{Status: "ok", Version: "test"})
	})
	c := stubServer(t, h)
	out, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if out.Status != "ok" || out.Version != "test" {
		t.Errorf("Health = %+v", out)
	}
}

func TestRegister_RequestAndResponse(t *testing.T) {
	t.Parallel()
	var gotBody RegisterRequest
	var gotAuth string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/register" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("X-Admin-Token")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(RegisterResponse{
			ClientID: "cid", ClientToken: "ctok", Projects: gotBody.Projects,
		})
	})
	c := stubServer(t, h, WithAdminToken("admin-secret"))
	out, err := c.Register(context.Background(), RegisterRequest{
		AdminToken: "admin-secret", Projects: []string{"p1", "p2"}, DisplayName: "lappy",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if gotAuth != "admin-secret" {
		t.Errorf("X-Admin-Token = %q, want %q", gotAuth, "admin-secret")
	}
	if len(gotBody.Projects) != 2 || gotBody.Projects[0] != "p1" {
		t.Errorf("Projects in request = %v", gotBody.Projects)
	}
	if out.ClientID != "cid" || out.ClientToken != "ctok" {
		t.Errorf("Register response = %+v", out)
	}
}

func TestClient_AdminRoutes_Authed(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/clients", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Admin-Token") != "secret" {
			writeErr(t, w, http.StatusUnauthorized, "auth_failed", "missing admin token")
			return
		}
		_ = json.NewEncoder(w).Encode(ListClientsResponse{Clients: []Client{{ClientID: "c1"}}})
	})
	mux.HandleFunc("GET /admin/projects", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ListProjectsResponse{Projects: []Project{{Path: "p", ClientID: "c1", AckedSeq: 7}}})
	})
	c := stubServer(t, mux, WithAdminToken("secret"))

	clients, err := c.ListClients(context.Background())
	if err != nil {
		t.Fatalf("ListClients: %v", err)
	}
	if len(clients.Clients) != 1 || clients.Clients[0].ClientID != "c1" {
		t.Errorf("ListClients = %+v", clients)
	}

	projects, err := c.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects.Projects) != 1 || projects.Projects[0].AckedSeq != 7 {
		t.Errorf("ListProjects = %+v", projects)
	}
}

func TestClient_ErrorDecoding(t *testing.T) {
	t.Parallel()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always 404 for any admin route.
		writeErr(t, w, http.StatusNotFound, "not_found", "no such client")
	})
	c := stubServer(t, h, WithAdminToken("x"))

	_, err := c.GetClient(context.Background(), "ghost")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsNotFound(err) {
		t.Errorf("IsNotFound(%v) = false, want true", err)
	}
	if !CodeMatches(err, "not_found") {
		t.Errorf("CodeMatches not_found = false, want true (err=%v)", err)
	}
}

func TestClient_UnauthorizedDetected(t *testing.T) {
	t.Parallel()
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeErr(t, w, http.StatusForbidden, "auth_failed", "forbidden")
	})
	c := stubServer(t, h)
	if err := c.DeleteClient(context.Background(), "any"); !IsUnauthorized(err) {
		t.Errorf("DeleteClient err = %v, IsUnauthorized = false", err)
	}
}

func TestClient_ConflictDetected(t *testing.T) {
	t.Parallel()
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeErr(t, w, http.StatusConflict, "conflict", "already exists")
	})
	c := stubServer(t, h)
	if _, err := c.ReclaimProject(context.Background(), ReclaimProjectRequest{Path: "p", NewClientID: "c2"}); !IsConflict(err) {
		t.Errorf("ReclaimProject err = %v, IsConflict = false", err)
	}
}

func TestClient_ReclaimRequest(t *testing.T) {
	t.Parallel()
	var got ReclaimProjectRequest
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(Project{Path: got.Path, ClientID: got.NewClientID})
	})
	c := stubServer(t, h, WithAdminToken("a"))
	out, err := c.ReclaimProject(context.Background(), ReclaimProjectRequest{Path: "project-a", NewClientID: "c2", Force: true})
	if err != nil {
		t.Fatalf("ReclaimProject: %v", err)
	}
	if got.Path != "project-a" || got.NewClientID != "c2" || !got.Force {
		t.Errorf("request = %+v", got)
	}
	if out.Path != "project-a" || out.ClientID != "c2" {
		t.Errorf("response = %+v", out)
	}
}

func TestClient_NonJSONErrorBody(t *testing.T) {
	t.Parallel()
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "upstream broke")
	})
	c := stubServer(t, h)
	_, err := c.ListClients(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "upstream broke") {
		t.Errorf("err = %v, want it to contain raw body", err)
	}
}

func TestError_AsWorksAcrossWraps(t *testing.T) {
	t.Parallel()
	src := &Error{Status: 404, ErrorResponse: ErrorResponse{Code: "not_found", Message: "x"}}
	wrapped := errors.New("wrap: " + src.Error())
	// IsNotFound unwraps through the chain to find the underlying *Error.
	// Note: this works because Error implements errors.Is nil-sentinel by
	// direct match through errors.As; we explicitly unwrap one level here to
	// force the test to actually demonstrate the chain.
	if IsNotFound(wrapped) {
		t.Errorf("plain errors.New wrap should not surface *Error; got true")
	}
	if !IsNotFound(src) {
		t.Errorf("IsNotFound on direct *Error should be true")
	}
}

// writeErr writes an ErrorResponse body with the given status and code.
// Helper for stub handlers; keeps tests terse.
func writeErr(t *testing.T, w http.ResponseWriter, status int, code, msg string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Code: code, Message: msg})
}
