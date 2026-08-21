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

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: all build gui gui-debug bindings snippet-bundle test test-gui vet clean fmt tidy watch watch-cli arch-pkg appimage dist

all: build

# Default: CLI + TUI only (no Wails/CGO/webkit dependency).
build:
	$(GO) build -o $(BINARY) ./cmd/wiretap

# GUI-enabled binary. `./wiretap gui` opens the dashboard.
gui:
	@echo "building with tags: $(GUI_TAGS) version=$(VERSION)"
	$(GO) build -trimpath -tags '$(GUI_TAGS)' \
		-ldflags "-s -w -X main.version=$(VERSION)" \
		-o $(BINARY) ./cmd/wiretap

# GUI build with Wails devtools enabled (inspector, console) during development.
gui-debug: GUI_TAGS := gui,debug$(WEBKIT_TAG)
gui-debug:
	@echo "building with tags: $(GUI_TAGS) version=$(VERSION)"
	$(GO) build -trimpath -tags '$(GUI_TAGS)' \
		-ldflags "-s -w -X main.version=$(VERSION)" \
		-o $(BINARY) ./cmd/wiretap

# Regenerate ui/bindings (requires the wails3 CLI; see gui_stub.go for how to
# build it with the gtk3 tag).
bindings:
	$(BINDINGS) ./cmd/wiretap ./internal/gui

# Rebuild the committed httpsnippet bundle embedded by internal/export
# (requires `npm install`; see internal/export/js/entry.js). Run after
# changing entry.js or bumping the httpsnippet-lite dependency.
snippet-bundle:
	npm run snippet-bundle

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
	rm -rf dist/ AppDir/

arch-pkg:
	@command -v makepkg >/dev/null 2>&1 || { \
			echo "makepkg not found; install pacman-contrib (or pacman on Arch) first" >&2; exit 1; }
	@if [ -z "$(VERSION)" ] || [ "$(VERSION)" = "dev" ]; then \
			echo "VERSION is unset or 'dev'; tag the commit (git tag vX.Y.Z) or set VERSION=vX.Y.Z" >&2; exit 1; \
	fi
	@ver=$(VERSION); ver=$${ver#v}; \
	basedir=$$(pwd); \
	tmp=$$(mktemp -d); cp packaging/arch/PKGBUILD "$$tmp/"; \
			sed -i "s/^pkgver=.*/pkgver=$$ver/" "$$tmp/PKGBUILD"; \
			chmod 644 "$$tmp/PKGBUILD"; \
			( cd "$$tmp" && makepkg -f --noconfirm ); \
			pkg="$$tmp/wiretap-*-x86_64.pkg.tar.zst"; \
			pkgfile=$$(ls -1 $$pkg 2>/dev/null | head -1); \
			if [ -z "$$pkgfile" ]; then echo "makepkg did not produce a package" >&2; rm -rf "$$tmp"; exit 1; fi; \
			out="$$basedir/dist/wiretap-$$ver-x86_64.pkg.tar.zst"; \
			mkdir -p "$$basedir/dist" && mv "$$pkgfile" "$$out"; \
			rm -rf "$$tmp"; \
			echo "wrote $$out"

appimage: gui
	@set -e; \
	if ! command -v appimagetool >/dev/null 2>&1; then \
		mkdir -p tools; \
		curl -fsSL -o tools/appimagetool \
			https://github.com/AppImage/AppImageKit/releases/download/continuous/appimagetool-x86_64.AppImage; \
		chmod +x tools/appimagetool; \
		export PATH="$$PWD/tools:$$PATH"; \
		echo "fetched appimagetool into tools/"; \
	fi; \
	if command -v rsvg-convert >/dev/null 2>&1; then \
		RSVG=rsvg-convert; \
	elif command -v magick >/dev/null 2>&1; then \
		RSVG='magick convert'; \
	elif command -v convert >/dev/null 2>&1; then \
		RSVG=convert; \
	else \
		echo 'install librsvg2-bin (rsvg-convert) or imagemagick to rasterize the icon' >&2; \
		exit 1; \
	fi; \
	ver=$(VERSION); ver=$${ver#v}; \
	out="dist/wiretap-$$ver-x86_64.AppImage"; \
	rm -rf AppDir; \
	mkdir -p AppDir/usr/bin AppDir/usr/share/applications AppDir/usr/share/icons/hicolor/256x256/apps AppDir/usr/share/icons/hicolor/512x512/apps; \
	install -m 0755 $(BINARY) AppDir/usr/bin/$(BINARY); \
	install -m 0644 packaging/linux/wiretap.desktop AppDir/wiretap.desktop; \
	install -m 0644 packaging/linux/wiretap.desktop AppDir/usr/share/applications/wiretap.desktop; \
	install -m 0755 packaging/appimage/AppRun AppDir/AppRun; \
	$$RSVG -w 256 -h 256 packaging/linux/wiretap.svg -o AppDir/wiretap.png; \
	install -m 0644 AppDir/wiretap.png AppDir/usr/share/icons/hicolor/256x256/apps/wiretap.png; \
	$$RSVG -w 512 -h 512 packaging/linux/wiretap.svg -o AppDir/usr/share/icons/hicolor/512x512/apps/wiretap.png 2>/dev/null || true; \
	cp AppDir/usr/share/icons/hicolor/256x256/apps/wiretap.png AppDir/.DirIcon; \
	mkdir -p dist; \
	( cd AppDir && appimagetool --no-appstream . "../$$out" ); \
	echo "wrote $$out"

dist: arch-pkg appimage
