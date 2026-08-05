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
