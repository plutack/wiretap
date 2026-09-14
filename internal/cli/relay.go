package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/plutack/wiretap/internal/api"
	"github.com/plutack/wiretap/internal/app"
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
	cmd.Flags().StringSliceVarP(&projects, "projects", "p", nil, "optional initial project paths (use `relay projects add` later)")
	cmd.Flags().StringVarP(&displayName, "name", "n", "", "human-readable client display name")
	cmd.Flags().BoolVar(&saveCreds, "save", false, "save credentials to ~/.config/wiretap/relay-credentials.json")
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
		Short: "Delete a client and revoke its project subscriptions",
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
	cmd.AddCommand(newRelayProjectsAddCmd())
	cmd.AddCommand(newRelayProjectsRemoveCmd())
	cmd.AddCommand(newRelayProjectsCreateCmd())
	cmd.AddCommand(newRelayProjectsDeleteCmd())
	cmd.AddCommand(newRelayProjectsSubscribeCmd())
	cmd.AddCommand(newRelayProjectsUnsubscribeCmd())
	cmd.AddCommand(newRelayProjectsReclaimCmd())
	return cmd
}

func newRelayProjectsCreateCmd() *cobra.Command {
	var clientID string
	cmd := &cobra.Command{
		Use:   "create <path>",
		Short: "Create a project with an initial subscriber",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newRelayClient(cmd)
			if err != nil {
				return err
			}
			out, err := c.AssignProject(cmd.Context(), strings.Trim(args[0], "/"), clientID)
			if err != nil {
				return err
			}
			printJSON(cmd, out)
			return nil
		},
	}
	cmd.Flags().StringVar(&clientID, "client-id", "", "initial subscriber client ID (required)")
	_ = cmd.MarkFlagRequired("client-id")
	return cmd
}

func newRelayProjectsDeleteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete <path>",
		Short: "Permanently delete a project and its relay history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				return fmt.Errorf("deleting a project permanently removes its relay history; pass --force to continue")
			}
			c, err := newRelayClient(cmd)
			if err != nil {
				return err
			}
			if err := c.DeleteProject(cmd.Context(), strings.Trim(args[0], "/")); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "deleted")
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "confirm deletion of the project and relay history")
	return cmd
}

// newRelaySelfClient uses this desktop's saved registration credentials for
// its own project changes. These commands do not require the admin token.
func newRelaySelfClient(cmd *cobra.Command) (*api.HTTPClient, *config.Credentials, error) {
	relayURL, _ := cmd.Flags().GetString("url")
	if strings.TrimSpace(relayURL) == "" {
		cfg, err := newConfigManager().Load()
		if err != nil {
			return nil, nil, fmt.Errorf("load config: %w", err)
		}
		relayURL = cfg.Relay.URL
	}
	base := app.IngressBaseURL(relayURL)
	if base == "" {
		return nil, nil, fmt.Errorf("no relay URL configured; register this desktop first")
	}
	creds, err := newConfigManager().LoadCredentials()
	if err != nil {
		return nil, nil, fmt.Errorf("load relay credentials: %w", err)
	}
	c, err := api.NewClient(base, api.WithClientAuth(creds.ClientID, creds.ClientToken))
	return c, creds, err
}

func newRelayProjectsAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <path>",
		Short: "Add a project to this registered client without rotating credentials",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, creds, err := newRelaySelfClient(cmd)
			if err != nil {
				return err
			}
			out, err := c.AddClientProject(cmd.Context(), strings.Trim(args[0], "/"))
			if err != nil {
				return err
			}
			creds.Projects = out.Projects
			if err := newConfigManager().SaveCredentials(*creds); err != nil {
				return fmt.Errorf("project added on relay, but saving credentials failed: %w", err)
			}
			printJSON(cmd, out)
			return nil
		},
	}
}

func newRelayProjectsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <path>",
		Short: "Unsubscribe this client without deleting project history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, creds, err := newRelaySelfClient(cmd)
			if err != nil {
				return err
			}
			out, err := c.RemoveClientProject(cmd.Context(), strings.Trim(args[0], "/"))
			if err != nil {
				return err
			}
			creds.Projects = out.Projects
			if err := newConfigManager().SaveCredentials(*creds); err != nil {
				return fmt.Errorf("project removed on relay, but saving credentials failed: %w", err)
			}
			printJSON(cmd, out)
			return nil
		},
	}
}

func newRelayProjectsSubscribeCmd() *cobra.Command {
	var clientID string
	var includeHistory bool
	cmd := &cobra.Command{
		Use:   "subscribe <path>",
		Short: "Add a client subscriber to an existing project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newRelayClient(cmd)
			if err != nil {
				return err
			}
			out, err := c.AddProjectSubscriber(cmd.Context(), strings.Trim(args[0], "/"), clientID, includeHistory)
			if err != nil {
				return err
			}
			printJSON(cmd, out)
			return nil
		},
	}
	cmd.Flags().StringVar(&clientID, "client-id", "", "client ID to subscribe (required)")
	cmd.Flags().BoolVar(&includeHistory, "include-history", false, "deliver retained history to the new subscriber")
	_ = cmd.MarkFlagRequired("client-id")
	return cmd
}

func newRelayProjectsUnsubscribeCmd() *cobra.Command {
	var clientID string
	cmd := &cobra.Command{
		Use:   "unsubscribe <path>",
		Short: "Remove a client subscriber without deleting the project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newRelayClient(cmd)
			if err != nil {
				return err
			}
			if err := c.RemoveProjectSubscriber(cmd.Context(), strings.Trim(args[0], "/"), clientID); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "unsubscribed")
			return nil
		},
	}
	cmd.Flags().StringVar(&clientID, "client-id", "", "client ID to unsubscribe (required)")
	_ = cmd.MarkFlagRequired("client-id")
	return cmd
}

func newRelayProjectsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all relay project paths",
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
	var clientID string
	var force bool

	cmd := &cobra.Command{
		Use:   "reclaim <path>",
		Short: "Replace all project subscribers with one client",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newRelayClient(cmd)
			if err != nil {
				return err
			}
			resp, err := c.ReclaimProject(cmd.Context(), api.ReclaimProjectRequest{
				Path:        args[0],
				NewClientID: clientID,
				Force:       force,
			})
			if err != nil {
				return err
			}
			printJSON(cmd, resp)
			return nil
		},
	}
	cmd.Flags().StringVar(&clientID, "client-id", "", "replacement client ID (required)")
	cmd.Flags().BoolVar(&force, "force", false, "replace all existing project subscribers")
	_ = cmd.MarkFlagRequired("client-id")
	return cmd
}

// ---- webhooks ----

func newRelayWebhooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "webhooks",
		Short: "Webhook inspection, replay, and retention management",
	}
	cmd.AddCommand(newRelayWebhooksListCmd())
	cmd.AddCommand(newRelayWebhooksReplayCmd())
	cmd.AddCommand(newRelayWebhooksDeleteCmd())
	return cmd
}

func newRelayWebhooksDeleteCmd() *cobra.Command {
	var all bool
	var through int64
	cmd := &cobra.Command{
		Use:   "delete <project> [seq...]",
		Short: "Delete selected, ranged, or all retained relay webhooks",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newRelayClient(cmd)
			if err != nil {
				return err
			}
			req := api.DeleteWebhooksRequest{All: all, ThroughSeq: through}
			for _, raw := range args[1:] {
				seq, err := strconv.ParseInt(raw, 10, 64)
				if err != nil || seq <= 0 {
					return fmt.Errorf("invalid seq %q", raw)
				}
				req.Seqs = append(req.Seqs, seq)
			}
			modes := 0
			if all {
				modes++
			}
			if through > 0 {
				modes++
			}
			if len(req.Seqs) > 0 {
				modes++
			}
			if modes != 1 {
				return fmt.Errorf("choose exactly one of sequence arguments, --through, or --all")
			}
			out, err := c.DeleteWebhooks(cmd.Context(), strings.Trim(args[0], "/"), req)
			if err != nil {
				return err
			}
			printJSON(cmd, out)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "delete all retained webhooks for the project")
	cmd.Flags().Int64Var(&through, "through", 0, "delete webhooks through this sequence, inclusive")
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
	var clientID string
	cmd := &cobra.Command{
		Use:   "replay <project> <seq>",
		Short: "Re-push a stored webhook to connected subscribers",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newRelayClient(cmd)
			if err != nil {
				return err
			}
			seq, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil || seq <= 0 {
				return fmt.Errorf("invalid seq %q", args[1])
			}
			if clientID != "" {
				err = c.ReplayWebhookToClient(cmd.Context(), args[0], seq, clientID)
			} else {
				err = c.ReplayWebhook(cmd.Context(), args[0], seq)
			}
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "replayed")
			return nil
		},
	}
	cmd.Flags().StringVar(&clientID, "client-id", "", "replay only to this connected subscriber")
	return cmd
}

// ---- helpers ----

// printJSON writes v as indented JSON to the command's stdout.
func printJSON(cmd *cobra.Command, v any) {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// saveCredentials persists the registration through the same manager used by
// every other CLI path. Production prefers the system keyring for the token;
// headless systems retain the mode-0600 compatibility file.
func saveCredentials(resp *api.RegisterResponse) error {
	return newConfigManager().SaveCredentials(config.Credentials{
		ClientID: resp.ClientID, ClientToken: resp.ClientToken, Projects: resp.Projects,
	})
}
