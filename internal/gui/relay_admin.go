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

// RelayAdminDeleteClientInput revokes a relay identity. Relay storage cascades
// the deletion to that client's project bindings and queued webhook history.
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
	Path      string `json:"path"`
	ClientID  string `json:"client_id"`
	CreatedAt int64  `json:"created_at"`
	AckedSeq  int64  `json:"acked_seq"`
}

// RelayAdminCredentialsView contains a newly-created client token. The relay
// returns it once, so the GUI keeps it only in component memory for copying.
type RelayAdminCredentialsView struct {
	ClientID    string   `json:"client_id"`
	ClientToken string   `json:"client_token"`
	Projects    []string `json:"projects"`
}

// RelayAdminOverview authenticates to the configured relay and returns its
// health, registered clients, and project ownership in one snapshot.
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
	client, _, err := relayAdminClient(in.RelayURL, in.AdminToken)
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
	return RelayAdminCredentialsView{
		ClientID: out.ClientID, ClientToken: out.ClientToken, Projects: out.Projects,
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

// RelayAdminReassignProject transfers project ownership. Existing ownership
// requires Force, which the GUI sets only after an explicit confirmation.
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
	return RelayAdminProjectView{
		Path: project.Path, ClientID: project.ClientID,
		CreatedAt: project.CreatedAt, AckedSeq: project.AckedSeq,
	}
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
