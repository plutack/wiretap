# Installation

wiretap is Linux-first. Prebuilt release binaries are the intended installation method because users should not need the Go or GUI build toolchains.

## Release installer

A website-hosted installer is planned as the standard installation path:

```sh
curl -fsSL https://wiretap.dev/install.sh | sh
```

Do not use this command yet: the domain, installer, release workflow, and signed release assets have not been published. Once available, the installer should:

- detect the operating system and CPU architecture;
- download a pinned release rather than source from the default branch;
- verify a published checksum before installation;
- install `wiretap` without modifying shell startup files;
- clearly report whether the installed binary includes GUI support;
- support a non-interactive version override and installation directory.

Piping a remote script into a shell is convenient but carries trust risk. Users should also be able to download and inspect the script before running it.

## Install a release binary manually

When releases are published, download the archive for your platform from the GitHub Releases page, verify its checksum, and install the binary somewhere on `PATH`:

```sh
sudo install -m 0755 wiretap /usr/local/bin/wiretap
wiretap version
```

A GUI-enabled Linux release requires WebKitGTK at runtime. Distribution package names vary; current distributions generally provide `webkit2gtk-4.1` or an equivalent package.

## Install a release AppImage

Linux AppImages are published alongside the `.tar.gz` for each release. The AppImage bundles the same all-in-one binary plus a desktop entry; runtime still requires WebKitGTK and GTK3 on the host (AppImages do not bundle glibc/GTK across distributions).

```sh
curl -fsSL -o wiretap.AppImage https://github.com/plutack/wiretap/releases/latest/download/wiretap_<ver>_x86_64.AppImage
chmod +x wiretap.AppImage
./wiretap.AppImage gui               # launches the GUI directly
```

To integrate the AppImage with the desktop environment (icon, `.desktop` file), put it under `~/.local/bin/wiretap.AppImage` and let AppImageUpdate or your file manager register it; or wrap it with a small script that calls the AppImage and adds a desktop entry pointing at it.

## Install from the Arch Linux package

Arch users on `x86_64` can install `wiretap` from the `.pkg.tar.zst` artifact attached to each release. Download directly from the GitHub Releases page:

```sh
sudo pacman -U wiretap-<ver>-x86_64.pkg.tar.zst
wiretap gui
```

The package ships the same GUI binary plus a desktop entry, and depends on `webkit2gtk-4.1` and `gtk3` from the official repos.

## Build from source

Source builds are intended for contributors and platforms without a published binary.

### CLI and TUI

Install the Go version declared in `.go-version`, then run:

```sh
git clone https://github.com/plutack/wiretap.git
cd wiretap
make build
sudo install -m 0755 wiretap /usr/local/bin/wiretap
```

`make build` produces the CLI/TUI binary and does not require WebKitGTK.

The equivalent direct command is:

```sh
go build -o wiretap ./cmd/wiretap
```

### Desktop GUI

A GUI build additionally requires CGO, a C compiler, `pkg-config`, and WebKitGTK development files. Install the appropriate WebKitGTK 4.1 or 4.0 development package for your distribution, then run:

```sh
make gui
sudo install -m 0755 wiretap /usr/local/bin/wiretap
wiretap gui
```

The Makefile detects the available WebKitGTK pkg-config package and selects the matching Wails build tag. Keeping this logic in `make gui` avoids requiring contributors to remember platform-specific build tags.

For a GUI build with WebView developer tools:

```sh
make gui-debug
```

### Local distribution artifacts

The Makefile wraps the same packaging steps the release workflow uses, so you can reproduce any artifact locally:

```sh
# AppImage for the current host (uses `make gui` underneath, then appimagetool).
# Requires librsvg2-bin (rsvg-convert); appimagetool is fetched into tools/ on
# first run if it isn't on $PATH.
make appimage VERSION=v0.1.0   # → dist/wiretap-v0.1.0-x86_64.AppImage

# Arch package (requires makepkg; rewrites packaging/arch/PKGBUILD's pkgver
# to match VERSION and renames the produced artifact to the release filename).
make arch-pkg VERSION=v0.1.0    # → dist/wiretap-v0.1.0-x86_64.pkg.tar.zst

# Both artifacts in one shot (sanity check before tagging a release).
make dist VERSION=v0.1.0
```

If `VERSION` is omitted, the targets read it from `git describe --tags`; tag the commit before running them so the artifact names match what CI will produce.

## Build the hosted relay

The relay is a separate server binary:

```sh
go build -trimpath -ldflags "-s -w" -o wiretap-relay ./cmd/wiretap-relay
sudo install -m 0755 wiretap-relay /usr/local/bin/wiretap-relay
```

Continue with the [relay hosting guide](HOSTING.md) to create its service account, systemd unit, storage directory, TLS reverse proxy, and admin token.

## Initialize wiretap

After installation:

```sh
wiretap config init
wiretap gui
```

Use `wiretap tui` when the installed binary does not include GUI support or when working over SSH.
