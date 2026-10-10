package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/plutack/wiretap/internal/logging"
	"github.com/spf13/cobra"
)

// Wrap actual execution, not persistent hooks: cleanup must run on failures as
// well as success, after command-owned workers and stores have been stopped.
func configureClientLogging(root *cobra.Command, version string) {
	root.PersistentFlags().String("log-format", "", "diagnostic format: text or json (WIRETAP_LOG_FORMAT; default text)")
	root.PersistentFlags().String("log-level", "", "diagnostic level (WIRETAP_LOG_LEVEL; default info)")
	root.PersistentFlags().String("log-file", "", "diagnostic file (WIRETAP_LOG_FILE; default <config-dir>/logs/<command>.log)")
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		if run := cmd.RunE; run != nil {
			cmd.RunE = func(cmd *cobra.Command, args []string) (result error) {
				value := func(flag, env, fallback string) string {
					v, _ := cmd.Flags().GetString(flag)
					if v != "" {
						return v
					}
					if v = os.Getenv(env); v != "" {
						return v
					}
					return fallback
				}
				path := value("log-file", "WIRETAP_LOG_FILE", "")
				if path == "" {
					dir, err := newConfigManager().Dir()
					if err != nil {
						return err
					}
					path = filepath.Join(dir, "logs", strings.ReplaceAll(cmd.CommandPath(), " ", "-")+".log")
				}
				if path == "-" {
					return fmt.Errorf("client logs require a file; terminal output would disrupt the TUI")
				}
				logger, sink, err := logging.OpenFile(path, value("log-format", "WIRETAP_LOG_FORMAT", "text"), value("log-level", "WIRETAP_LOG_LEVEL", "info"))
				if err != nil {
					return err
				}
				logger = logger.With("component", "client", "version", version, "pid", os.Getpid(), "command", cmd.CommandPath())
				previous := cmd.Context()
				cmd.SetContext(logging.WithContext(previous, logger))
				defer cmd.SetContext(previous)
				logger.Info("client_command_started")
				defer func() {
					if result != nil {
						logger.Error("client_command_failed")
					} else {
						logger.Info("client_command_completed")
					}
					result = errors.Join(result, sink.Close())
				}()
				return run(cmd, args)
			}
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
}
