# wiretap Makefile. Default target builds the CLI/TUI binary (no GUI deps).
# `make gui` builds the GUI-enabled binary, auto-selecting the Wails v3 webkit
# tag for this system (GTK3/webkit2gtk-4.1 on most current distros, GTK4/
# webkitgtk-6.0 where available).

BINARY   := wiretap
GO       := go
GOFLAGS  := -race -shuffle=on

# Auto-detect the Wails v3 webkit build tag for this system. v3 targets
# webkitgtk-6.0 (GTK4) by default; the `gtk3` tag switches to
# webkit2gtk-4.1, which is what most current distros actually ship.
WEBKIT_TAG := $(shell \
	if pkg-config --exists webkitgtk-6.0; then echo ""; \
	elif pkg-config --exists webkit2gtk-4.1; then echo ",gtk3"; \
	else echo ",gtk3"; fi)

# Tags for a production GUI build: gui (our gate) + production (Wails release
# mode, vs. devtools-enabled dev mode) + the webkit selector.
GUI_TAGS := gui,production$(WEBKIT_TAG)

# Regenerate the frontend bindings after changing internal/gui.
BINDINGS := wails3 generate bindings -b -noevents -names -d ui/bindings -f '-tags gui'

# air config: GUI dev by default (.air.toml), CLI dev via .air.cli.toml.
AIR := air
AIR_CLI := air -c .air.cli.toml

.PHONY: all build gui gui-debug bindings test test-gui vet clean fmt tidy watch watch-cli

all: build

# Default: CLI + TUI only (no Wails/CGO/webkit dependency).
build:
	$(GO) build -o $(BINARY) ./cmd/wiretap

# GUI-enabled binary. `./wiretap gui` opens the dashboard.
gui:
	@echo "building with tags: $(GUI_TAGS)"
	$(GO) build -tags '$(GUI_TAGS)' -o $(BINARY) ./cmd/wiretap

# GUI build with Wails devtools enabled (inspector, console) during development.
gui-debug: GUI_TAGS := gui,debug$(WEBKIT_TAG)
gui-debug:
	@echo "building with tags: $(GUI_TAGS)"
	$(GO) build -tags '$(GUI_TAGS)' -o $(BINARY) ./cmd/wiretap

# Regenerate ui/bindings (requires the wails3 CLI; see gui_stub.go for how to
# build it with the gtk3 tag).
bindings:
	$(BINDINGS) ./cmd/wiretap ./internal/gui

# Full test suite (default build; the GUI launch glue is covered by the build).
test:
	$(GO) test $(GOFLAGS) ./...

# Run the CLI tests + the internal/gui binding tests explicitly.
test-gui:
	$(GO) test $(GOFLAGS) ./internal/gui/...

vet:
	$(GO) vet ./...
	$(GO) vet -tags '$(GUI_TAGS)' ./...

fmt:
	@gofmt -l . | tee /dev/stderr | grep -q . && exit 1 || true

tidy:
	$(GO) mod tidy

# Live-reload dev workflows (air). Rebuilds + relaunches on file changes.
# GUI dev: watches .go + ui/ files, recompiles Tailwind before each build,
# launches the Wails window. Requires the GUI toolchain (same deps as `make gui`).
watch:
	$(AIR)

# CLI/TUI dev: watches .go files, launches `wiretap tui`. No GUI deps needed.
watch-cli:
	$(AIR_CLI)

clean:
	rm -f $(BINARY)
