package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/plutack/wiretap/internal/config"
)

func newConfigImportCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "import <client-file>",
		Short: "Import a portable relay client identity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("read relay client file %s: %w", args[0], err)
			}
			clientFile, err := config.ParseRelayClientFile(raw)
			if err != nil {
				return err
			}
			m := newConfigManager()
			if err := m.ImportRelayClient(clientFile, force); err != nil {
				return err
			}
			stored, err := m.LoadCredentials()
			if err != nil {
				return fmt.Errorf("verify imported relay credentials: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "imported relay client %s\n", stored.ClientID)
			fmt.Fprintf(cmd.OutOrStdout(), "relay tunnel: %s\n", clientFile.RelayURL)
			fmt.Fprintf(cmd.OutOrStdout(), "token storage: %s\n", stored.TokenStorage)
			fmt.Fprintln(cmd.OutOrStdout(), "the import file contains a plaintext bearer token; delete it when the handoff is complete")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "replace a different locally configured relay client identity")
	return cmd
}
