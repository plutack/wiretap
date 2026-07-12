package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/plutack/wiretap/internal/api"
	"github.com/plutack/wiretap/internal/config"
)

// newRelayCmd groups all `wiretap relay` subcommands — the admin surface for
// talking to a wiretap-relay instance over HTTP. Each subcommand wraps one
// api.HTTPClient method and pretty-prints the JSON result. Flags override
// config-file values so callers can target a different relay without editing
// on disk.
func newRelayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "relay",
		Short: "Relay administration (register, clients, projects, webhooks)",
	}
	cmd.PersistentFlags().String("url", "", "relay base URL (default: from config)")
	cmd.PersistentFlags().String("admin-token", "", "admin token for /register and /admin/* (default: from config)")

	cmd.AddCommand(newRelayRegisterCmd())
	cmd.AddCommand(newRelayClientsCmd())
	cmd.AddCommand(newRelayProjectsCmd())
	cmd.AddCommand(newRelayWebhooksCmd())
	return cmd
}

// newRelayClient builds an api.HTTPClient from flags + config. The factory
// is a test seam: tests override it to return a client pointed at
// httptest.NewServer.
//
//nolint:gochecknoglobals // test seam, same pattern as newConfigManager
var newRelayClient = func(cmd *cobra.Command) (*api.HTTPClient, error) {
	urlFlag, _ := cmd.Flags().GetString("url")
	tokFlag, _ := cmd.Flags().GetString("admin-token")

	// Fall back to config when flags are empty.
	cfg, err := newConfigManager().Load()
	if err != nil {
		// Config file missing — only an error when flags aren't set.
		if urlFlag == "" {
			return nil, fmt.Errorf("no relay URL: pass --url or run `wiretap config init`")
		}
	} else {
		if urlFlag == "" {
			urlFlag = cfg.Relay.URL
		}
		if tokFlag == "" {
			// Admin token isn't in the config file yet; for now it's
			// flag-only. The config schema can grow an admin_token field
			// later.
		}
	}

	opts := []api.ClientOption{}
	if tokFlag != "" {
		opts = append(opts, api.WithAdminToken(tokFlag))
	}
	return api.NewClient(urlFlag, opts...)
}

// ---- register ----

func newRelayRegisterCmd() *cobra.Command {
	var projects []string
	var displayName string
	var saveCreds bool

	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register this PC as a new relay client and claim project paths",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newRelayClient(cmd)
			if err != nil {
				return err
			}
			tok, _ := cmd.Flags().GetString("admin-token")
			resp, err := c.Register(cmd.Context(), api.RegisterRequest{
				AdminToken:  tok,
				Projects:    projects,
				DisplayName: displayName,
			})
			if err != nil {
				return err
			}
			if saveCreds {
				if err := saveCredentials(resp); err != nil {
					return fmt.Errorf("save credentials: %w", err)
				}
			}
			printJSON(cmd, resp)
			return nil
		},
	}
	cmd.Flags().StringSliceVarP(&projects, "projects", "p", nil, "project paths to claim (required)")
	cmd.Flags().StringVarP(&displayName, "name", "n", "", "human-readable client display name")
	cmd.Flags().BoolVar(&saveCreds, "save", false, "save credentials to ~/.config/wiretap/relay-credentials.json")
	_ = cmd.MarkFlagRequired("projects")
	return cmd
}

// ---- clients ----

func newRelayClientsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clients",
		Short: "Client administration",
	}
	cmd.AddCommand(newRelayClientsListCmd())
	cmd.AddCommand(newRelayClientsGetCmd())
	cmd.AddCommand(newRelayClientsDeleteCmd())
	return cmd
}

func newRelayClientsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all registered clients",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newRelayClient(cmd)
			if err != nil {
				return err
			}
			resp, err := c.ListClients(cmd.Context())
			if err != nil {
				return err
			}
			printJSON(cmd, resp)
			return nil
		},
	}
}

func newRelayClientsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <client-id>",
		Short: "Show details for one client",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newRelayClient(cmd)
			if err != nil {
				return err
			}
			resp, err := c.GetClient(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			printJSON(cmd, resp)
			return nil
		},
	}
}

func newRelayClientsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <client-id>",
		Short: "Delete a client (cascades to project bindings)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newRelayClient(cmd)
			if err != nil {
				return err
			}
			if err := c.DeleteClient(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "deleted")
			return nil
		},
	}
}

// ---- projects ----

func newRelayProjectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "Project administration",
	}
	cmd.AddCommand(newRelayProjectsListCmd())
	cmd.AddCommand(newRelayProjectsReclaimCmd())
	return cmd
}

func newRelayProjectsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all claimed project paths",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newRelayClient(cmd)
			if err != nil {
				return err
			}
			resp, err := c.ListProjects(cmd.Context())
			if err != nil {
				return err
			}
			printJSON(cmd, resp)
			return nil
		},
	}
}

func newRelayProjectsReclaimCmd() *cobra.Command {
	var newClientID string
	var force bool

	cmd := &cobra.Command{
		Use:   "reclaim <path>",
		Short: "Reclaim a project path for a different client",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newRelayClient(cmd)
			if err != nil {
				return err
			}
			resp, err := c.ReclaimProject(cmd.Context(), api.ReclaimProjectRequest{
				Path:        args[0],
				NewClientID: newClientID,
				Force:       force,
			})
			if err != nil {
				return err
			}
			printJSON(cmd, resp)
			return nil
		},
	}
	cmd.Flags().StringVar(&newClientID, "new-client", "", "client ID to rebind the path to (required)")
	cmd.Flags().BoolVar(&force, "force", false, "reclaim even if the path is already owned by another client")
	_ = cmd.MarkFlagRequired("new-client")
	return cmd
}

// ---- webhooks ----

func newRelayWebhooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "webhooks",
		Short: "Webhook inspection and replay",
	}
	cmd.AddCommand(newRelayWebhooksListCmd())
	cmd.AddCommand(newRelayWebhooksReplayCmd())
	return cmd
}

func newRelayWebhooksListCmd() *cobra.Command {
	var afterSeq int64
	var limit int64

	cmd := &cobra.Command{
		Use:   "list <project>",
		Short: "List stored webhooks for a project (paginated)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newRelayClient(cmd)
			if err != nil {
				return err
			}
			resp, err := c.ListWebhooks(cmd.Context(), args[0], afterSeq, limit)
			if err != nil {
				return err
			}
			printJSON(cmd, resp)
			return nil
		},
	}
	cmd.Flags().Int64Var(&afterSeq, "after-seq", 0, "only return webhooks with seq > this value")
	cmd.Flags().Int64Var(&limit, "limit", 50, "maximum webhooks to return (0 = server default)")
	return cmd
}

func newRelayWebhooksReplayCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "replay <project> <seq>",
		Short: "Re-push a stored webhook to the owning client over its tunnel",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newRelayClient(cmd)
			if err != nil {
				return err
			}
			var seq int64
			if _, err := fmt.Sscanf(args[1], "%d", &seq); err != nil {
				return fmt.Errorf("invalid seq %q: %w", args[1], err)
			}
			if err := c.ReplayWebhook(cmd.Context(), args[0], seq); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "replayed")
			return nil
		},
	}
}

// ---- helpers ----

// printJSON writes v as indented JSON to the command's stdout.
func printJSON(cmd *cobra.Command, v any) {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// saveCredentials writes the RegisterResponse to the credentials file so
// relayclient can load it on startup. The file is JSON with client_id,
// client_token, and projects. Mode 0600 to keep the token secret.
func saveCredentials(resp *api.RegisterResponse) error {
	m := config.NewManager()
	dir, err := m.Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "relay-credentials.json")
	b, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
