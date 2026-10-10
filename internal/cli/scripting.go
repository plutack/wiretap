package cli

import (
	"github.com/plutack/wiretap/internal/scripting"
	"log/slog"
)

func newScriptEngine() *scripting.Engine { return scripting.New() }

// Script names, source and exception text are user-controlled and may contain
// captured secrets. Record safe stage metadata, never print during TUI rendering.
func scriptErrorLogger(logger *slog.Logger) func(scripting.Trigger, string, error) {
	return func(trigger scripting.Trigger, name string, err error) {
		stage := "run"
		if name == "" {
			stage = "load"
		}
		logger.Warn("script_failed", "trigger", string(trigger), "stage", stage)
	}
}
