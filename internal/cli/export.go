package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/plutack/wiretap/internal/app"
)

// newExportCmd groups `wiretap export` subcommands: render stored captures
// and webhooks as ready-to-run code snippets via the embedded httpsnippet
// engine (internal/export). The same engine backs the GUI's "Export as code"
// panel, so both frontends produce identical snippets.
func newExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export stored requests as code snippets (curl, fetch, python, …)",
		Long: "Render a stored traffic capture or webhook as a ready-to-run code snippet.\n\n" +
			"The snippet language is selected with --as target[/client], e.g. shell/curl,\n" +
			"javascript/fetch, python/requests, go. Omitting the client uses the target's\n" +
			"default. Run `wiretap export targets` for the full catalog.",
	}
	cmd.PersistentFlags().String("as", "shell/curl", "snippet format: target[/client]")
	cmd.AddCommand(newExportTargetsCmd())
	cmd.AddCommand(newExportCaptureCmd())
	cmd.AddCommand(newExportWebhookCmd())
	return cmd
}

// newExportApp opens the composition root for read-only export access. No
// tunnel is started — exports work entirely from the local store.
func newExportApp(cmd *cobra.Command) (*app.App, error) {
	a := app.New(newConfigManager())
	if err := a.Open(cmd.Context()); err != nil {
		return nil, fmt.Errorf("export: open store: %w", err)
	}
	return a, nil
}

// parseAs splits the --as flag (target[/client]) into its parts.
func parseAs(cmd *cobra.Command) (target, client string) {
	spec, _ := cmd.Flags().GetString("as")
	target, client, _ = strings.Cut(spec, "/")
	return strings.TrimSpace(target), strings.TrimSpace(client)
}

func newExportTargetsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "targets",
		Short: "List available snippet targets and clients",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ts, err := app.New(newConfigManager()).ExportTargets()
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			for _, t := range ts {
				clients := make([]string, 0, len(t.Clients))
				for _, c := range t.Clients {
					key := c.Key
					if key == t.DefaultClient {
						key += "*"
					}
					clients = append(clients, key)
				}
				fmt.Fprintf(w, "%-12s %s\n", t.Key, strings.Join(clients, ", "))
			}
			fmt.Fprintln(w, "\n* = default client when --as omits one")
			return nil
		},
	}
}

func newExportCaptureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "capture <id>",
		Short: "Export a traffic capture's request as a code snippet",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid capture id %q: %w", args[0], err)
			}
			a, err := newExportApp(cmd)
			if err != nil {
				return err
			}
			defer a.Close()
			target, client := parseAs(cmd)
			out, err := a.ExportCapture(cmd.Context(), id, target, client)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		},
	}
}

func newExportWebhookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "webhook <project> <seq>",
		Short: "Export a stored webhook delivery as a code snippet",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			seq, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid seq %q: %w", args[1], err)
			}
			a, err := newExportApp(cmd)
			if err != nil {
				return err
			}
			defer a.Close()
			target, client := parseAs(cmd)
			out, err := a.ExportWebhook(cmd.Context(), args[0], seq, target, client)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		},
	}
}
