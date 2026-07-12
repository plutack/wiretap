// Command wiretap is the local app: CLI + TUI + (later) Wails GUI host.
// main.go is intentionally tiny — it delegates the command tree to
// internal/cli so the logic stays testable without a main package.
package main

import (
	"fmt"
	"os"

	"github.com/plutack/wiretap/internal/cli"
)

// version is overridable at build time with:
//
//	go build -ldflags "-X main.version=1.2.3" ./cmd/wiretap
var version = "dev"

func main() {
	if err := cli.Execute(version); err != nil {
		fmt.Fprintln(os.Stderr, "wiretap:", err)
		os.Exit(1)
	}
}
