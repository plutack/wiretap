---
title: Install Wiretap
description: Download Wiretap for Linux or Windows, verify it, or build it from source.
---

Prebuilt release artifacts are the recommended installation path. The current release is **v0.2.15**.

:::caution[Release trust]
Wiretap publishes SHA-256 checksums, but the release artifacts are not yet cryptographically signed. A checksum detects a damaged download only when you obtain the checksum from a trusted channel.
:::

## Linux

Choose the artifact for your machine from [the v0.2.15 release](https://github.com/plutack/wiretap/releases/tag/v0.2.15):

| Format | Architectures | Best for |
| --- | --- | --- |
| `.tar.gz` | x86-64, ARM64 | CLI/TUI or manual installation |
| AppImage | x86-64, ARM64 | Portable desktop GUI |
| Arch package | x86-64 | Pacman-managed desktop install |

For a tarball, extract the archive and install the binary on `PATH`:

```sh
tar -xzf wiretap_0.2.15_linux_amd64.tar.gz
sudo install -m 0755 wiretap /usr/local/bin/wiretap
wiretap version
```

For an AppImage:

```sh
chmod +x wiretap_0.2.15_x86_64.AppImage
./wiretap_0.2.15_x86_64.AppImage gui
```

The desktop build requires GTK 3 and WebKitGTK at runtime. Package names vary by distribution; install the WebKitGTK 4.1 runtime supplied by your distribution if the GUI does not start.

On Arch Linux:

```sh
sudo pacman -U wiretap-0.2.15-x86_64.pkg.tar.zst
wiretap gui
```

## Windows

Download the x86-64 installer from [the v0.2.15 release](https://github.com/plutack/wiretap/releases/tag/v0.2.15), or use the `.zip` when you want a portable binary. Windows on ARM is not currently published.

## Verify a download

Download `SHA256SUMS` beside the artifact, then run:

```sh
sha256sum --check SHA256SUMS --ignore-missing
```

PowerShell users can compare a file with the matching line in `SHA256SUMS`:

```powershell
Get-FileHash .\wiretap_0.2.15_windows_x86_64-installer.exe -Algorithm SHA256
```

## Build from source

Source builds are useful for contributors or unsupported platforms. Install the Go version declared in `.go-version`, then clone the repository:

```sh
git clone https://github.com/plutack/wiretap.git
cd wiretap
make build
sudo install -m 0755 wiretap /usr/local/bin/wiretap
```

`make build` produces the CLI/TUI build. A desktop build additionally needs CGO, a C compiler, `pkg-config`, GTK 3, and WebKitGTK development files:

```sh
make gui
sudo install -m 0755 wiretap /usr/local/bin/wiretap
```

## Initialize

Create a configuration file, then open the desktop:

```sh
wiretap config init
wiretap gui
```

Configuration is optional for local interception; Wiretap uses loopback-only defaults when no config file exists.

Next, [capture your first request](/getting-started/quick-start/).
