package gui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/plutack/wiretap/internal/api"
	"github.com/plutack/wiretap/internal/app"
	"github.com/plutack/wiretap/internal/config"
	"github.com/plutack/wiretap/internal/secretstore"
)

const relayAdminTimeout = 15 * time.Second

// RelayAdminInput carries the ephemeral credentials for one relay admin
// operation. The admin token crosses the Wails bridge for the request only;
// wiretap never writes it to config or credentials storage.
type RelayAdminInput struct {
	RelayURL   string `json:"relay_url"`
	AdminToken string `json:"admin_token"`
}

type RelayAdminSaveProfileInput struct {
	RelayURL   string `json:"relay_url"`
	AdminToken string `json:"admin_token"`
	Name       string `json:"name"`
}

type RelayAdminProfileView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	RelayURL string `json:"relay_url"`
	LastUsed int64  `json:"last_used"`
}

// RelayAdminCreateClientInput creates credentials for another relay client.
// Unlike RegisterRelay, it does not replace this desktop's saved identity.
type RelayAdminCreateClientInput struct {
	RelayURL    string   `json:"relay_url"`
	AdminToken  string   `json:"admin_token"`
	DisplayName string   `json:"display_name"`
	Projects    []string `json:"projects"`
}

// RelayAdminDeleteClientInput revokes an identity and its subscriptions.
type RelayAdminDeleteClientInput struct {
	RelayURL   string `json:"relay_url"`
	AdminToken string `json:"admin_token"`
	ClientID   string `json:"client_id"`
}

// RelayAdminAddProjectInput assigns a new project path to an existing client.
type RelayAdminAddProjectInput struct {
	RelayURL   string `json:"relay_url"`
	AdminToken string `json:"admin_token"`
	Path       string `json:"path"`
	ClientID   string `json:"client_id"`
}

// RelayAdminDeleteProjectInput permanently removes a project binding and its
// queued relay-side webhook history.
type RelayAdminDeleteProjectInput struct {
	RelayURL   string `json:"relay_url"`
	AdminToken string `json:"admin_token"`
	Path       string `json:"path"`
}

// RelayAdminReassignProjectInput moves a path to another registered client.
type RelayAdminReassignProjectInput struct {
	RelayURL    string `json:"relay_url"`
	AdminToken  string `json:"admin_token"`
	Path        string `json:"path"`
	NewClientID string `json:"new_client_id"`
	Force       bool   `json:"force"`
}

type RelayAdminSubscriptionInput struct {
	RelayURL       string `json:"relay_url"`
	AdminToken     string `json:"admin_token"`
	Path           string `json:"path"`
	ClientID       string `json:"client_id"`
	IncludeHistory bool   `json:"include_history"`
}

type RelayAdminListWebhooksInput struct {
	RelayURL   string `json:"relay_url"`
	AdminToken string `json:"admin_token"`
	Path       string `json:"path"`
	AfterSeq   int64  `json:"after_seq"`
	Limit      int64  `json:"limit"`
}

type RelayAdminDeleteWebhooksInput struct {
	RelayURL   string  `json:"relay_url"`
	AdminToken string  `json:"admin_token"`
	Path       string  `json:"path"`
	Seqs       []int64 `json:"seqs"`
	ThroughSeq int64   `json:"through_seq"`
	All        bool    `json:"all"`
}

// RelayAdminOverviewView is the bounded server-management snapshot rendered
// by Settings. It deliberately contains no client or admin secret.
type RelayAdminOverviewView struct {
	BaseURL     string                  `json:"base_url"`
	Status      string                  `json:"status"`
	Version     string                  `json:"version"`
	TunnelCount int                     `json:"tunnel_count"`
	Clients     []RelayAdminClientView  `json:"clients"`
	Projects    []RelayAdminProjectView `json:"projects"`
}

type RelayAdminClientView struct {
	ClientID    string   `json:"client_id"`
	DisplayName string   `json:"display_name,omitempty"`
	CreatedAt   int64    `json:"created_at"`
	LastSeenAt  int64    `json:"last_seen_at,omitempty"`
	Projects    []string `json:"projects,omitempty"`
}

type RelayAdminProjectView struct {
	Path          string                       `json:"path"`
	CreatedAt     int64                        `json:"created_at"`
	NextSeq       int64                        `json:"next_seq"`
	WebhookCount  int64                        `json:"webhook_count"`
	Subscriptions []RelayAdminSubscriptionView `json:"subscriptions"`
	ClientID      string                       `json:"client_id,omitempty"`
	AckedSeq      int64                        `json:"acked_seq,omitempty"`
}

type RelayAdminSubscriptionView struct {
	ClientID  string `json:"client_id"`
	CreatedAt int64  `json:"created_at"`
	StartSeq  int64  `json:"start_seq"`
	AckedSeq  int64  `json:"acked_seq"`
	Pending   int64  `json:"pending"`
}

type RelayAdminWebhookView struct {
	Seq        int64  `json:"seq"`
	ReceivedAt int64  `json:"received_at"`
	SourceIP   string `json:"source_ip,omitempty"`
	Method     string `json:"method"`
	Path       string `json:"path,omitempty"`
	BodyBytes  int    `json:"body_bytes"`
}

type RelayAdminWebhookPageView struct {
	Webhooks     []RelayAdminWebhookView `json:"webhooks"`
	NextAfterSeq int64                   `json:"next_after_seq,omitempty"`
}

// RelayAdminCredentialsView contains a newly-created client token. The relay
// returns it once, so the GUI keeps it only in component memory for copying or
// downloading as a portable handoff.
type RelayAdminCredentialsView struct {
	Format      string   `json:"format"`
	Version     int      `json:"version"`
	RelayURL    string   `json:"relay_url"`
	ClientID    string   `json:"client_id"`
	ClientToken string   `json:"client_token"`
	Projects    []string `json:"projects"`
}

// RelayAdminOverview returns relay health, clients, projects, and subscriptions.
func (b *Bindings) RelayAdminOverview(in RelayAdminInput) (RelayAdminOverviewView, error) {
	client, base, err := relayAdminClient(in.RelayURL, in.AdminToken)
	if err != nil {
		return RelayAdminOverviewView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), relayAdminTimeout)
	defer cancel()
	health, err := client.Health(ctx)
	if err != nil {
		return RelayAdminOverviewView{}, fmt.Errorf("relay health: %w", err)
	}
	clients, err := client.ListClients(ctx)
	if err != nil {
		return RelayAdminOverviewView{}, fmt.Errorf("list relay clients: %w", err)
	}
	projects, err := client.ListProjects(ctx)
	if err != nil {
		return RelayAdminOverviewView{}, fmt.Errorf("list relay projects: %w", err)
	}
	view := RelayAdminOverviewView{
		BaseURL: base, Status: health.Status, Version: health.Version,
		TunnelCount: health.TunnelCount,
		Clients:     make([]RelayAdminClientView, 0, len(clients.Clients)),
		Projects:    make([]RelayAdminProjectView, 0, len(projects.Projects)),
	}
	for _, client := range clients.Clients {
		view.Clients = append(view.Clients, RelayAdminClientView{
			ClientID: client.ClientID, DisplayName: client.DisplayName,
			CreatedAt: client.CreatedAt, LastSeenAt: client.LastSeenAt, Projects: client.Projects,
		})
	}
	for _, project := range projects.Projects {
		view.Projects = append(view.Projects, relayAdminProjectView(project))
	}
	return view, nil
}

func (b *Bindings) RelayAdminProfiles() ([]RelayAdminProfileView, error) {
	profiles, err := b.app.LoadRelayAdminProfiles()
	if err != nil {
		return nil, err
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].LastUsed > profiles[j].LastUsed })
	out := make([]RelayAdminProfileView, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, relayAdminProfileView(profile))
	}
	return out, nil
}

// RelayAdminSaveProfile stores non-secret relay metadata on disk and the
// privileged token in the operating system credential store.
func (b *Bindings) RelayAdminSaveProfile(in RelayAdminSaveProfileInput) (RelayAdminProfileView, error) {
	token := strings.TrimSpace(in.AdminToken)
	if token == "" {
		return RelayAdminProfileView{}, errors.New("save relay profile: admin token is required")
	}
	_, base, err := relayAdminClient(in.RelayURL, token)
	if err != nil {
		return RelayAdminProfileView{}, err
	}
	profile := config.RelayAdminProfile{ID: relayAdminProfileID(base), Name: strings.TrimSpace(in.Name), RelayURL: base, LastUsed: time.Now().Unix()}
	if profile.Name == "" {
		if parsed, parseErr := url.Parse(base); parseErr == nil {
			profile.Name = parsed.Hostname()
		}
	}
	if profile.Name == "" {
		profile.Name = base
	}
	if err := b.secrets.Set(relayAdminProfileAccount(profile.ID), token); err != nil {
		return RelayAdminProfileView{}, fmt.Errorf("save relay profile in system keyring: %w", err)
	}
	// Verify the same account can be read before committing profile metadata.
	// Some Secret Service collection configurations accept a write but cannot
	// rediscover it after reopening; without this check they leave a saved relay
	// that can never reconnect.
	verified, err := b.secrets.Get(relayAdminProfileAccount(profile.ID))
	if err != nil || verified != token {
		_ = b.secrets.Delete(relayAdminProfileAccount(profile.ID))
		if err == nil {
			err = errors.New("stored token did not match")
		}
		return RelayAdminProfileView{}, fmt.Errorf("verify relay profile in system keyring: %w", err)
	}
	profiles, err := b.app.LoadRelayAdminProfiles()
	if err != nil {
		_ = b.secrets.Delete(relayAdminProfileAccount(profile.ID))
		return RelayAdminProfileView{}, err
	}
	updated := false
	for i := range profiles {
		if profiles[i].ID == profile.ID {
			profiles[i] = profile
			updated = true
			break
		}
	}
	if !updated {
		profiles = append(profiles, profile)
	}
	if err := b.app.SaveRelayAdminProfiles(profiles); err != nil {
		_ = b.secrets.Delete(relayAdminProfileAccount(profile.ID))
		return RelayAdminProfileView{}, err
	}
	return relayAdminProfileView(profile), nil
}

// RelayAdminLoadProfile retrieves a saved token and marks the profile used.
func (b *Bindings) RelayAdminLoadProfile(id string) (RelayAdminInput, error) {
	id = strings.TrimSpace(id)
	profiles, err := b.app.LoadRelayAdminProfiles()
	if err != nil {
		return RelayAdminInput{}, err
	}
	for i := range profiles {
		if profiles[i].ID != id {
			continue
		}
		token, err := b.secrets.Get(relayAdminProfileAccount(id))
		if err != nil {
			return RelayAdminInput{}, fmt.Errorf("load relay profile from system keyring: %w", err)
		}
		profiles[i].LastUsed = time.Now().Unix()
		if err := b.app.SaveRelayAdminProfiles(profiles); err != nil {
			return RelayAdminInput{}, err
		}
		return RelayAdminInput{RelayURL: profiles[i].RelayURL, AdminToken: token}, nil
	}
	return RelayAdminInput{}, errors.New("load relay profile: profile not found")
}

func (b *Bindings) RelayAdminDeleteProfile(id string) error {
	id = strings.TrimSpace(id)
	profiles, err := b.app.LoadRelayAdminProfiles()
	if err != nil {
		return err
	}
	found := false
	kept := profiles[:0]
	for _, profile := range profiles {
		if profile.ID == id {
			found = true
			continue
		}
		kept = append(kept, profile)
	}
	if !found {
		return errors.New("delete relay profile: profile not found")
	}
	if err := b.secrets.Delete(relayAdminProfileAccount(id)); err != nil && !errors.Is(err, secretstore.ErrNotFound) {
		return fmt.Errorf("delete relay profile from system keyring: %w", err)
	}
	return b.app.SaveRelayAdminProfiles(kept)
}

func relayAdminProfileID(base string) string {
	sum := sha256.Sum256([]byte(base))
	return hex.EncodeToString(sum[:12])
}
func relayAdminProfileAccount(id string) string { return "relay/" + id }
func relayAdminProfileView(profile config.RelayAdminProfile) RelayAdminProfileView {
	return RelayAdminProfileView{ID: profile.ID, Name: profile.Name, RelayURL: profile.RelayURL, LastUsed: profile.LastUsed}
}

// RelayAdminCreateClient creates portable credentials without changing the
// desktop currently registered in wiretap.
func (b *Bindings) RelayAdminCreateClient(in RelayAdminCreateClientInput) (RelayAdminCredentialsView, error) {
	client, base, err := relayAdminClient(in.RelayURL, in.AdminToken)
	if err != nil {
		return RelayAdminCredentialsView{}, err
	}
	projects := normalizeRelayProjects(in.Projects)
	ctx, cancel := context.WithTimeout(context.Background(), relayAdminTimeout)
	defer cancel()
	out, err := client.Register(ctx, api.RegisterRequest{
		AdminToken: strings.TrimSpace(in.AdminToken), Projects: projects,
		DisplayName: strings.TrimSpace(in.DisplayName),
	})
	if err != nil {
		return RelayAdminCredentialsView{}, fmt.Errorf("create relay client: %w", err)
	}
	tunnelURL, err := app.TunnelURLFromBase(base)
	if err != nil {
		return RelayAdminCredentialsView{}, fmt.Errorf("create relay client file: %w", err)
	}
	clientFile, err := config.NewRelayClientFile(tunnelURL, out.ClientID, out.ClientToken, out.Projects)
	if err != nil {
		return RelayAdminCredentialsView{}, fmt.Errorf("create relay client file: %w", err)
	}
	return RelayAdminCredentialsView{
		Format: clientFile.Format, Version: clientFile.Version, RelayURL: clientFile.RelayURL,
		ClientID: clientFile.ClientID, ClientToken: clientFile.ClientToken, Projects: clientFile.Projects,
	}, nil
}

// RelayAdminDeleteClient revokes a client. The frontend owns the destructive
// confirmation so this method remains usable from generated bindings/tests.
func (b *Bindings) RelayAdminDeleteClient(in RelayAdminDeleteClientInput) error {
	client, _, err := relayAdminClient(in.RelayURL, in.AdminToken)
	if err != nil {
		return err
	}
	clientID := strings.TrimSpace(in.ClientID)
	if clientID == "" {
		return errors.New("delete relay client: client ID is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), relayAdminTimeout)
	defer cancel()
	if err := client.DeleteClient(ctx, clientID); err != nil {
		return fmt.Errorf("delete relay client: %w", err)
	}
	return nil
}

// RelayAdminAddProject creates a project for an existing client without
// registering another identity.
func (b *Bindings) RelayAdminAddProject(in RelayAdminAddProjectInput) (RelayAdminProjectView, error) {
	client, base, err := relayAdminClient(in.RelayURL, in.AdminToken)
	if err != nil {
		return RelayAdminProjectView{}, err
	}
	path := strings.Trim(strings.TrimSpace(in.Path), "/")
	clientID := strings.TrimSpace(in.ClientID)
	if path == "" || clientID == "" {
		return RelayAdminProjectView{}, errors.New("add relay project: project path and client ID are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), relayAdminTimeout)
	defer cancel()
	out, err := client.AssignProject(ctx, path, clientID)
	if err != nil {
		return RelayAdminProjectView{}, fmt.Errorf("add relay project: %w", err)
	}
	if err := b.syncLocalRelayProjects(ctx, base, client); err != nil {
		return RelayAdminProjectView{}, fmt.Errorf("project added, but this desktop could not synchronize: %w", err)
	}
	return relayAdminProjectView(*out), nil
}

func (b *Bindings) RelayAdminAddSubscriber(in RelayAdminSubscriptionInput) (RelayAdminProjectView, error) {
	client, base, err := relayAdminClient(in.RelayURL, in.AdminToken)
	if err != nil {
		return RelayAdminProjectView{}, err
	}
	path := strings.Trim(strings.TrimSpace(in.Path), "/")
	clientID := strings.TrimSpace(in.ClientID)
	if path == "" || clientID == "" {
		return RelayAdminProjectView{}, errors.New("add relay subscriber: project path and client ID are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), relayAdminTimeout)
	defer cancel()
	out, err := client.AddProjectSubscriber(ctx, path, clientID, in.IncludeHistory)
	if err != nil {
		return RelayAdminProjectView{}, fmt.Errorf("add relay subscriber: %w", err)
	}
	if err := b.syncLocalRelayProjects(ctx, base, client); err != nil {
		return RelayAdminProjectView{}, fmt.Errorf("subscriber added, but this desktop could not synchronize: %w", err)
	}
	return relayAdminProjectView(*out), nil
}

func (b *Bindings) RelayAdminRemoveSubscriber(in RelayAdminSubscriptionInput) error {
	client, base, err := relayAdminClient(in.RelayURL, in.AdminToken)
	if err != nil {
		return err
	}
	path := strings.Trim(strings.TrimSpace(in.Path), "/")
	clientID := strings.TrimSpace(in.ClientID)
	if path == "" || clientID == "" {
		return errors.New("remove relay subscriber: project path and client ID are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), relayAdminTimeout)
	defer cancel()
	if err := client.RemoveProjectSubscriber(ctx, path, clientID); err != nil {
		return fmt.Errorf("remove relay subscriber: %w", err)
	}
	if err := b.syncLocalRelayProjects(ctx, base, client); err != nil {
		return fmt.Errorf("subscriber removed, but this desktop could not synchronize: %w", err)
	}
	return nil
}

func (b *Bindings) RelayAdminListWebhooks(in RelayAdminListWebhooksInput) (RelayAdminWebhookPageView, error) {
	client, _, err := relayAdminClient(in.RelayURL, in.AdminToken)
	if err != nil {
		return RelayAdminWebhookPageView{}, err
	}
	path := strings.Trim(strings.TrimSpace(in.Path), "/")
	if path == "" {
		return RelayAdminWebhookPageView{}, errors.New("list relay webhooks: project path is required")
	}
	limit := in.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	ctx, cancel := context.WithTimeout(context.Background(), relayAdminTimeout)
	defer cancel()
	out, err := client.ListWebhookSummaries(ctx, path, in.AfterSeq, limit)
	if err != nil {
		return RelayAdminWebhookPageView{}, fmt.Errorf("list relay webhooks: %w", err)
	}
	view := RelayAdminWebhookPageView{NextAfterSeq: out.NextAfterSeq, Webhooks: make([]RelayAdminWebhookView, 0, len(out.Webhooks))}
	for _, webhook := range out.Webhooks {
		view.Webhooks = append(view.Webhooks, RelayAdminWebhookView{
			Seq: webhook.Seq, ReceivedAt: webhook.ReceivedAt, SourceIP: webhook.SourceIP,
			Method: webhook.Method, Path: webhook.Path, BodyBytes: webhook.BodyBytes,
		})
	}
	return view, nil
}

func (b *Bindings) RelayAdminDeleteWebhooks(in RelayAdminDeleteWebhooksInput) (int64, error) {
	client, _, err := relayAdminClient(in.RelayURL, in.AdminToken)
	if err != nil {
		return 0, err
	}
	path := strings.Trim(strings.TrimSpace(in.Path), "/")
	if path == "" {
		return 0, errors.New("delete relay webhooks: project path is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), relayAdminTimeout)
	defer cancel()
	out, err := client.DeleteWebhooks(ctx, path, api.DeleteWebhooksRequest{
		Seqs: in.Seqs, ThroughSeq: in.ThroughSeq, All: in.All,
	})
	if err != nil {
		return 0, fmt.Errorf("delete relay webhooks: %w", err)
	}
	return out.Deleted, nil
}

// RelayAdminDeleteProject deletes one project and its queued relay history.
func (b *Bindings) RelayAdminDeleteProject(in RelayAdminDeleteProjectInput) error {
	client, base, err := relayAdminClient(in.RelayURL, in.AdminToken)
	if err != nil {
		return err
	}
	path := strings.Trim(strings.TrimSpace(in.Path), "/")
	if path == "" {
		return errors.New("delete relay project: project path is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), relayAdminTimeout)
	defer cancel()
	if err := client.DeleteProject(ctx, path); err != nil {
		return fmt.Errorf("delete relay project: %w", err)
	}
	if err := b.syncLocalRelayProjects(ctx, base, client); err != nil {
		return fmt.Errorf("project deleted, but this desktop could not synchronize: %w", err)
	}
	return nil
}

// RelayAdminReassignProject preserves the legacy replace-all-subscribers API.
func (b *Bindings) RelayAdminReassignProject(in RelayAdminReassignProjectInput) (RelayAdminProjectView, error) {
	client, base, err := relayAdminClient(in.RelayURL, in.AdminToken)
	if err != nil {
		return RelayAdminProjectView{}, err
	}
	path := strings.Trim(strings.TrimSpace(in.Path), "/")
	clientID := strings.TrimSpace(in.NewClientID)
	if path == "" || clientID == "" {
		return RelayAdminProjectView{}, errors.New("reassign relay project: project path and new client ID are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), relayAdminTimeout)
	defer cancel()
	out, err := client.ReclaimProject(ctx, api.ReclaimProjectRequest{
		Path: path, NewClientID: clientID, Force: in.Force,
	})
	if err != nil {
		return RelayAdminProjectView{}, fmt.Errorf("reassign relay project: %w", err)
	}
	if err := b.syncLocalRelayProjects(ctx, base, client); err != nil {
		return RelayAdminProjectView{}, fmt.Errorf("project moved, but this desktop could not synchronize: %w", err)
	}
	return relayAdminProjectView(*out), nil
}

func (b *Bindings) syncLocalRelayProjects(ctx context.Context, adminBase string, client *api.HTTPClient) error {
	cfg, err := b.app.Config()
	if err != nil || app.IngressBaseURL(cfg.Relay.URL) != adminBase {
		return nil
	}
	creds, err := b.app.RelayCredentials()
	if err != nil || creds.ClientID == "" {
		return nil
	}
	current, err := client.GetClient(ctx, creds.ClientID)
	if err != nil {
		if api.IsNotFound(err) {
			return nil
		}
		return err
	}
	want := append([]string(nil), current.Projects...)
	have := append([]string(nil), creds.Projects...)
	sort.Strings(want)
	sort.Strings(have)
	if slices.Equal(want, have) {
		return nil
	}
	creds.Projects = want
	if err := b.app.SaveRelayCredentials(*creds); err != nil {
		return err
	}
	return b.app.RestartTunnel(context.Background())
}

func relayAdminProjectView(project api.Project) RelayAdminProjectView {
	view := RelayAdminProjectView{
		Path: project.Path, CreatedAt: project.CreatedAt, NextSeq: project.NextSeq,
		WebhookCount: project.WebhookCount, ClientID: project.ClientID, AckedSeq: project.AckedSeq,
		Subscriptions: make([]RelayAdminSubscriptionView, 0, len(project.Subscriptions)),
	}
	for _, sub := range project.Subscriptions {
		view.Subscriptions = append(view.Subscriptions, RelayAdminSubscriptionView{
			ClientID: sub.ClientID, CreatedAt: sub.CreatedAt, StartSeq: sub.StartSeq,
			AckedSeq: sub.AckedSeq, Pending: sub.Pending,
		})
	}
	// Older relays return only the legacy single-owner fields.
	if len(view.Subscriptions) == 0 && project.ClientID != "" {
		view.Subscriptions = append(view.Subscriptions, RelayAdminSubscriptionView{
			ClientID: project.ClientID, AckedSeq: project.AckedSeq,
		})
	}
	return view
}

func relayAdminClient(rawURL, adminToken string) (*api.HTTPClient, string, error) {
	token := strings.TrimSpace(adminToken)
	if token == "" {
		return nil, "", errors.New("relay admin: admin token is required")
	}
	tunnelURL, err := app.TunnelURLFromBase(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("relay admin: %w", err)
	}
	base := app.IngressBaseURL(tunnelURL)
	if base == "" {
		return nil, "", fmt.Errorf("relay admin: cannot derive HTTP URL from %q", rawURL)
	}
	client, err := api.NewClient(base, api.WithAdminToken(token))
	if err != nil {
		return nil, "", fmt.Errorf("relay admin: %w", err)
	}
	return client, base, nil
}

func normalizeRelayProjects(projects []string) []string {
	out := make([]string, 0, len(projects))
	seen := make(map[string]struct{}, len(projects))
	for _, project := range projects {
		project = strings.Trim(strings.TrimSpace(project), "/")
		if project == "" {
			continue
		}
		if _, exists := seen[project]; exists {
			continue
		}
		seen[project] = struct{}{}
		out = append(out, project)
	}
	return out
}
