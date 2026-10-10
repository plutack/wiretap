package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutack/wiretap/internal/logging"
	"github.com/plutack/wiretap/internal/scripting"
	"github.com/spf13/cobra"
)

func TestClientDiagnosticsNeverWriteToCommandTerminal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.log")
	root := &cobra.Command{Use: "wiretap", SilenceErrors: true, SilenceUsage: true}
	child := &cobra.Command{Use: "tui", RunE: func(cmd *cobra.Command, _ []string) error {
		logger := logging.FromContext(cmd.Context())
		logger.Info("background_event")
		scriptErrorLogger(logger)(scripting.OnReplay, "secret-script-name", fmt.Errorf("secret-exception"))
		fmt.Fprintln(cmd.OutOrStdout(), "tui-render-only")
		return nil
	}}
	root.AddCommand(child)
	configureClientLogging(root, "test-version")
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"tui", "--log-file", path, "--log-format", "json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "tui-render-only\n" || stderr.Len() != 0 {
		t.Fatal("diagnostics corrupted terminal", stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"background_event", "script_failed", "client_command_completed", "test-version"} {
		if !strings.Contains(string(data), event) {
			t.Fatal("missing", event)
		}
	}
	for _, secret := range []string{"secret-script-name", "secret-exception"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("leaked", secret)
		}
	}
}

func TestClientLoggingFlagsOverrideEnvironment(t *testing.T) {
	t.Setenv("WIRETAP_LOG_FORMAT", "invalid")
	t.Setenv("WIRETAP_LOG_LEVEL", "invalid")
	root := &cobra.Command{Use: "wiretap", RunE: func(*cobra.Command, []string) error { return nil }}
	configureClientLogging(root, "test")
	root.SetArgs([]string{"--log-file", filepath.Join(t.TempDir(), "test.log"), "--log-format", "json", "--log-level", "error"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestClientLoggingRejectsTerminalAndUnwritableSink(t *testing.T) {
	for _, path := range []string{"-", t.TempDir()} {
		ran := false
		root := &cobra.Command{Use: "wiretap", SilenceErrors: true, SilenceUsage: true, RunE: func(*cobra.Command, []string) error { ran = true; return nil }}
		configureClientLogging(root, "test")
		root.SetArgs([]string{"--log-file", path})
		if err := root.Execute(); err == nil || ran {
			t.Fatal("unsafe sink accepted", path)
		}
	}
}
