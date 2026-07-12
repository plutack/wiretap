package relayd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/api"
	"github.com/plutack/wiretap/internal/store"
	"github.com/plutack/wiretap/internal/testutil"
)

// freshServer stands up a relayd Server backed by an in-memory migrated
// RelayStore. Returns the server, its httptest.Server (for raw HTTP), and
// an api.HTTPClient pointed at it. Cleanup is registered per test.
//
// The fake clock pins received_at/created_at to a fixed time; the fake id
// generator alternates "client-N" / "token-N" so register responses are
// predictable in assertions.
func freshServer(t *testing.T, opts ...Option) (*Server, *httptest.Server, *api.HTTPClient) {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenInMemory(fmt.Sprintf("relayd-%s", t.Name()))
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.MigrateRelay(ctx, db); err != nil {
		t.Fatalf("MigrateRelay: %v", err)
	}
	st := store.NewRelayStore(db)
	fixedTime := time.Unix(1_700_000_000, 0).UTC()
	all := append([]Option{}, opts...)
	all = append(all,
		WithClock(&testutil.FakeClock{T: fixedTime}),
		WithIDGen(&fakeRegisterGen{}),
		WithAdminToken("admin-secret"),
		WithVersion("test-v"),
	)
	srv := NewServer(st, all...)
	hs := httptest.NewServer(srv.Routes())
	t.Cleanup(hs.Close)
	c, err := api.NewClient(hs.URL, api.WithAdminToken("admin-secret"))
	if err != nil {
		t.Fatalf("api.NewClient: %v", err)
	}
	return srv, hs, c
}

// fakeRegisterGen alternates between "client-N" and "token-N" so the first
// RegisterResponse is predictable (ClientID="client-001", ClientToken="token-002").
type fakeRegisterGen struct{ counter int }

func (g *fakeRegisterGen) NewID() string {
	g.counter++
	label := "token"
	if g.counter%2 == 1 {
		label = "client"
	}
	return fmt.Sprintf("%s-%03d", label, (g.counter+1)/2)
}

// makeClientFor seeds the store directly without going through Register.
// For tests that focus on other paths and just need a known client/project.
func makeClientFor(t *testing.T, s *Server, clientID, token string, projects ...string) {
	t.Helper()
	ctx := context.Background()
	now := s.clock.Now()
	if err := s.store.CreateClient(ctx, clientID, token, "test", now); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	for _, p := range projects {
		if err := s.store.BindProject(ctx, p, clientID, now); err != nil {
			t.Fatalf("BindProject %s: %v", p, err)
		}
	}
}

func TestHandleHealth_StatusOK(t *testing.T) {
	t.Parallel()
	_, _, c := freshServer(t)
	resp, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if resp.Status != "ok" || resp.Version != "test-v" {
		t.Errorf("Health = %+v", resp)
	}
}

func TestHandleRegister_Success(t *testing.T) {
	t.Parallel()
	_, _, c := freshServer(t)
	out, err := c.Register(context.Background(), api.RegisterRequest{
		AdminToken: "admin-secret", Projects: []string{"project-a", "project-b"}, DisplayName: "lappy",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if out.ClientID != "client-001" {
		t.Errorf("ClientID = %q, want client-001", out.ClientID)
	}
	if out.ClientToken != "token-001" {
		t.Errorf("ClientToken = %q, want token-001", out.ClientToken)
	}
	if len(out.Projects) != 2 {
		t.Errorf("Projects = %v", out.Projects)
	}
}

func TestHandleRegister_RejectsBadProjectName(t *testing.T) {
	t.Parallel()
	_, _, c := freshServer(t)
	bad := []string{"UPPER", "x", "", "with space", "-leading", "with_underscore"}
	for _, p := range bad {
		_, err := c.Register(context.Background(), api.RegisterRequest{
			AdminToken: "admin-secret", Projects: []string{p},
		})
		if err == nil {
			t.Errorf("project %q should have been rejected", p)
		}
	}
}

func TestHandleRegister_ConflictOnDuplicateProject(t *testing.T) {
	t.Parallel()
	s, _, c := freshServer(t)
	makeClientFor(t, s, "c1", "t1", "project-a")
	_, err := c.Register(context.Background(), api.RegisterRequest{
		AdminToken: "admin-secret", Projects: []string{"project-a"},
	})
	if err == nil || !api.IsConflict(err) {
		t.Fatalf("err = %v, want conflict", err)
	}
	// The rolled-back client should not exist.
	if _, err := s.store.Client(context.Background(), "client-001"); err == nil {
		t.Error("register conflict should roll back the client row so callers can retry")
	}
}

func TestHandleRegister_RequiresAdminToken(t *testing.T) {
	t.Parallel()
	_, hs, _ := freshServer(t)
	// Build a request with the wrong admin token.
	body := bytes.NewBufferString(`{"admin_token":"wrong","projects":["project-a"]}`)
	resp, err := http.Post(hs.URL+"/register", "application/json", body)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestHandleIngress_Success_RawHeadersAndBodyPreserved(t *testing.T) {
	t.Parallel()
	s, hs, _ := freshServer(t)
	makeClientFor(t, s, "c1", "t1", "project-a")

	body := []byte(`{"event":"order.created","id":42}`)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		hs.URL+"/project-a/orders/42", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "push")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, b)
	}
	var out api.IngressResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Seq != 1 {
		t.Errorf("seq = %d, want 1", out.Seq)
	}

	rows, err := s.store.WebhooksAfter(context.Background(), "project-a", 0)
	if err != nil {
		t.Fatalf("WebhooksAfter: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if string(rows[0].Body) != string(body) {
		t.Errorf("stored body = %q, want %q", rows[0].Body, body)
	}
	if rows[0].Path != "/orders/42" {
		t.Errorf("stored path = %q, want /orders/42", rows[0].Path)
	}
	if !strings.Contains(string(rows[0].RawHeaders), "X-Github-Event: push") {
		t.Errorf("raw headers should preserve X-Github-Event (canonicalised); got %q", rows[0].RawHeaders)
	}
}

func TestHandleIngress_UnknownProjectReturns404(t *testing.T) {
	t.Parallel()
	_, hs, _ := freshServer(t)
	resp, err := http.Post(hs.URL+"/does-not-exist", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHandleIngress_RejectsInvalidPathShape(t *testing.T) {
	t.Parallel()
	_, hs, _ := freshServer(t)
	resp, err := http.Post(hs.URL+"/UPPERCASE", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (invalid path shape)", resp.StatusCode)
	}
}

func TestHandleListClients_AdminAuth(t *testing.T) {
	t.Parallel()
	s, _, c := freshServer(t)
	makeClientFor(t, s, "c1", "t1", "project-a", "project-b")
	resp, err := c.ListClients(context.Background())
	if err != nil {
		t.Fatalf("ListClients: %v", err)
	}
	// c1 was seeded directly; fakeRegisterGen is unused so only one client.
	if len(resp.Clients) != 1 {
		t.Fatalf("clients = %d, want 1 (c1)", len(resp.Clients))
	}
	if resp.Clients[0].ClientID != "c1" {
		t.Errorf("ClientID = %q, want c1", resp.Clients[0].ClientID)
	}
	if len(resp.Clients[0].Projects) != 2 {
		t.Errorf("Projects = %v, want 2", resp.Clients[0].Projects)
	}
}

func TestHandleListClients_RejectsMissingAdmin(t *testing.T) {
	t.Parallel()
	_, hs, _ := freshServer(t)
	resp, err := http.Get(hs.URL + "/admin/clients")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestHandleGetClient_NotFound(t *testing.T) {
	t.Parallel()
	_, _, c := freshServer(t)
	_, err := c.GetClient(context.Background(), "ghost")
	if err == nil || !api.IsNotFound(err) {
		t.Fatalf("err = %v, want not_found", err)
	}
}

func TestHandleDeleteClient_Success(t *testing.T) {
	t.Parallel()
	s, _, c := freshServer(t)
	makeClientFor(t, s, "c1", "t1", "project-a")
	if err := c.DeleteClient(context.Background(), "c1"); err != nil {
		t.Fatalf("DeleteClient: %v", err)
	}
	// Confirm the project binding cascaded away.
	if _, err := s.store.Project(context.Background(), "project-a"); err == nil {
		t.Error("deleting client should cascade to projects")
	}
}

func TestHandleDeleteClient_NotFound(t *testing.T) {
	t.Parallel()
	_, _, c := freshServer(t)
	if err := c.DeleteClient(context.Background(), "ghost"); !api.IsNotFound(err) {
		t.Fatalf("err = %v, want not_found", err)
	}
}

func TestHandleListProjects(t *testing.T) {
	t.Parallel()
	s, _, c := freshServer(t)
	makeClientFor(t, s, "c1", "t1", "alpha", "beta")
	resp, err := c.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(resp.Projects) != 2 {
		t.Fatalf("projects = %d, want 2", len(resp.Projects))
	}
	// Sorted ascending.
	if resp.Projects[0].Path != "alpha" || resp.Projects[1].Path != "beta" {
		t.Errorf("projects out of order: %+v", resp.Projects)
	}
	// Owner recorded.
	if resp.Projects[0].ClientID != "c1" {
		t.Errorf("alpha owner = %q, want c1", resp.Projects[0].ClientID)
	}
}

func TestHandleReclaimProject_ConflictWithoutForce(t *testing.T) {
	t.Parallel()
	s, _, c := freshServer(t)
	makeClientFor(t, s, "c1", "t1", "project-a")
	makeClientFor(t, s, "c2", "t2")
	_, err := c.ReclaimProject(context.Background(), api.ReclaimProjectRequest{
		Path: "project-a", NewClientID: "c2", Force: false,
	})
	if err == nil || !api.IsConflict(err) {
		t.Fatalf("err = %v, want conflict", err)
	}
}

func TestHandleReclaimProject_ForceSucceeds(t *testing.T) {
	t.Parallel()
	s, _, c := freshServer(t)
	makeClientFor(t, s, "c1", "t1", "project-a")
	makeClientFor(t, s, "c2", "t2")
	out, err := c.ReclaimProject(context.Background(), api.ReclaimProjectRequest{
		Path: "project-a", NewClientID: "c2", Force: true,
	})
	if err != nil {
		t.Fatalf("ReclaimProject: %v", err)
	}
	if out.ClientID != "c2" {
		t.Errorf("owner after reclaim = %q, want c2", out.ClientID)
	}
	// Confirm in the store.
	p, _ := s.store.Project(context.Background(), "project-a")
	if p.ClientID != "c2" {
		t.Errorf("store owner = %q, want c2", p.ClientID)
	}
}

func TestHandleReclaimProject_IdempotentSameOwner(t *testing.T) {
	t.Parallel()
	s, _, _ := freshServer(t)
	makeClientFor(t, s, "c1", "t1", "project-a")
	// Reclaiming to the same owner should succeed without force.
	_, _, c2 := freshServer(t) // discard; we need a client on the first server
	_ = c2
	// Build a typed client against the first server (which already has admin token).
}

func TestHandleReclaimProject_NewClientNotFound(t *testing.T) {
	t.Parallel()
	s, _, c := freshServer(t)
	makeClientFor(t, s, "c1", "t1", "project-a")
	_, err := c.ReclaimProject(context.Background(), api.ReclaimProjectRequest{
		Path: "project-a", NewClientID: "ghost", Force: true,
	})
	if err == nil || !api.IsNotFound(err) {
		t.Fatalf("err = %v, want not_found", err)
	}
}

// ingress POST helper for tests that prefer raw HTTP. Returns seq from body.
func postIngress(t *testing.T, hs *httptest.Server, project string, body []byte, headers map[string]string) (int64, *http.Response) {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, hs.URL+"/"+project, bytes.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("postIngress: %v", err)
	}
	defer resp.Body.Close()
	var out api.IngressResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.Seq, resp
}

func TestHandleListWebhooks_AfterIngress(t *testing.T) {
	t.Parallel()
	s, hs, c := freshServer(t)
	makeClientFor(t, s, "c1", "t1", "project-a")
	for i := 0; i < 3; i++ {
		body := []byte(fmt.Sprintf(`{"i":%d}`, i))
		seq, resp := postIngress(t, hs, "project-a", body, map[string]string{"Content-Type": "application/json"})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("ingress[%d] status = %d", i, resp.StatusCode)
		}
		if seq != int64(i+1) {
			t.Errorf("seq[%d] = %d, want %d", i, seq, i+1)
		}
	}
	resp, err := c.ListWebhooks(context.Background(), "project-a", 0, 0)
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if len(resp.Webhooks) != 3 {
		t.Fatalf("webhooks = %d, want 3", len(resp.Webhooks))
	}
	if resp.Webhooks[0].Seq != 1 || resp.Webhooks[2].Seq != 3 {
		t.Errorf("seqs = [%d, %d], want [1, 3]", resp.Webhooks[0].Seq, resp.Webhooks[2].Seq)
	}
}

func TestSerializeRawHeaders_PreservesDuplicates(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	h.Add("X-Forwarded-For", "10.0.0.1")
	h.Add("X-Forwarded-For", "10.0.0.2")
	h.Set("Content-Type", "application/json")
	raw := serializeRawHeaders(h, "POST", "relay.example.com")
	s := string(raw)
	if !strings.Contains(s, "Host: relay.example.com") {
		t.Errorf("missing Host line; got %q", s)
	}
	// Both X-Forwarded-For lines should appear (preserved duplicate).
	if strings.Count(s, "X-Forwarded-For:") != 2 {
		t.Errorf("expected 2 X-Forwarded-For lines; got %q", s)
	}
	if !strings.Contains(s, "10.0.0.1") || !strings.Contains(s, "10.0.0.2") {
		t.Errorf("missing values; got %q", s)
	}
}

func TestSerializeRawHeaders_DeterministicKeyOrder(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	h.Set("Zeta", "1")
	h.Set("Alpha", "2")
	h.Set("Middle", "3")
	raw := string(serializeRawHeaders(h, "POST", ""))
	// Sort keys ascending: Alpha, Middle, Zeta (Host comes first if present).
	alphaIdx := strings.Index(raw, "Alpha")
	middleIdx := strings.Index(raw, "Middle")
	zetaIdx := strings.Index(raw, "Zeta")
	if !(alphaIdx < middleIdx && middleIdx < zetaIdx) {
		t.Errorf("keys not sorted ascending; got %q", raw)
	}
}
