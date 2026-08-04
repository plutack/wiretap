package cli

import (
	"fmt"
	"os"

	"github.com/plutack/wiretap/internal/scripting"
)

// newScriptEngine builds the JS scripting engine used across the CLI
// composition roots (intercept proxy, GUI, TUI). It uses the engine's default
// timeout; scripts are loaded per-trigger from the local store at run time, so
// a fresh engine with no scripts is a harmless identity pass.
func newScriptEngine() *scripting.Engine {
	return scripting.New()
}

// logScriptError is the shared OnScriptError sink: it prints per-script load
// and run errors to stderr so a broken payload script is visible without
// wedging traffic. Passed to intercept.Deps and app.WithScriptEngine.
func logScriptError(trigger scripting.Trigger, name string, err error) {
	if name == "" {
		fmt.Fprintf(os.Stderr, "wiretap: %s script load error: %v\n", trigger, err)
		return
	}
	fmt.Fprintf(os.Stderr, "wiretap: %s script %q error: %v\n", trigger, name, err)
}
