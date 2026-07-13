# wiretap Makefile. Default target builds the CLI/TUI binary (no GUI deps).
# `make gui` builds the GUI-enabled binary, auto-selecting the webkit2gtk tag
# for this system (4.1 on most current distros, 4.0 on older ones).

BINARY   := wiretap
GO       := go
GOFLAGS  := -race -shuffle=on

# Auto-detect the Wails webkit build tag for this system. Wails v2 gates its
# Linux frontend behind webkit2_36 / webkit2_40 / webkit2_41; without one it
# falls back to the legacy webkit2gtk-4.0 pkg-config path (which is often not
# installed when 4.1 is). Probe in 4.1 → 4.0 order.
WEBKIT_TAG := $(shell \
	if pkg-config --exists webkit2gtk-4.1; then echo webkit2_41; \
	elif pkg-config --exists webkit2gtk-4.0; then echo webkit2_40; \
	else echo webkit2_41; fi)

# Tags for a production GUI build: gui (our gate) + production (Wails real-app
# gate, vs. its stub) + the webkit selector.
GUI_TAGS := gui,production,$(WEBKIT_TAG)

# air config: GUI dev by default (.air.toml), CLI dev via .air.cli.toml.
AIR := air
AIR_CLI := air -c .air.cli.toml

.PHONY: all build gui gui-debug test test-gui vet clean fmt tidy watch watch-cli

all: build

# Default: CLI + TUI only (no Wails/CGO/webkit dependency).
build:
	$(GO) build -o $(BINARY) ./cmd/wiretap

# GUI-enabled binary. `./wiretap gui` opens the dashboard.
gui:
	@echo "building with tags: $(GUI_TAGS)"
	$(GO) build -tags '$(GUI_TAGS)' -o $(BINARY) ./cmd/wiretap

# GUI build with Wails devtools enabled (inspector, console) during development.
gui-debug: GUI_TAGS := gui,production,debug,$(WEBKIT_TAG)
gui-debug:
	@echo "building with tags: $(GUI_TAGS)"
	$(GO) build -tags '$(GUI_TAGS)' -o $(BINARY) ./cmd/wiretap

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