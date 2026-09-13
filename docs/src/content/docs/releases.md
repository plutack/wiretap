---
title: Releases and downloads
description: Download Wiretap v0.2.15, verify artifacts, and review recent changes.
---

## Current release: v0.2.15

Released September 13, 2026. This release adds the Starlight documentation site and renames the relay project-reclaim flag to `--client-id`.

[Open the v0.2.15 release](https://github.com/plutack/wiretap/releases/tag/v0.2.15) · [View every release](https://github.com/plutack/wiretap/releases) · [Download checksums](https://github.com/plutack/wiretap/releases/latest/download/SHA256SUMS)

### Assets

| Platform | Artifact |
| --- | --- |
| Linux x86-64 | [`wiretap_0.2.15_linux_amd64.tar.gz`](https://github.com/plutack/wiretap/releases/download/v0.2.15/wiretap_0.2.15_linux_amd64.tar.gz) |
| Linux ARM64 | [`wiretap_0.2.15_linux_arm64.tar.gz`](https://github.com/plutack/wiretap/releases/download/v0.2.15/wiretap_0.2.15_linux_arm64.tar.gz) |
| AppImage x86-64 | [`wiretap_0.2.15_x86_64.AppImage`](https://github.com/plutack/wiretap/releases/download/v0.2.15/wiretap_0.2.15_x86_64.AppImage) |
| AppImage ARM64 | [`wiretap_0.2.15_aarch64.AppImage`](https://github.com/plutack/wiretap/releases/download/v0.2.15/wiretap_0.2.15_aarch64.AppImage) |
| Arch Linux x86-64 | [`wiretap-0.2.15-x86_64.pkg.tar.zst`](https://github.com/plutack/wiretap/releases/download/v0.2.15/wiretap-0.2.15-x86_64.pkg.tar.zst) |
| Windows x86-64 | [`wiretap_0.2.15_windows_x86_64-installer.exe`](https://github.com/plutack/wiretap/releases/download/v0.2.15/wiretap_0.2.15_windows_x86_64-installer.exe) |
| Windows portable x86-64 | [`wiretap_0.2.15_windows_x86_64.zip`](https://github.com/plutack/wiretap/releases/download/v0.2.15/wiretap_0.2.15_windows_x86_64.zip) |

See [Install Wiretap](/getting-started/install/) for runtime dependencies and verification steps.

:::caution
Release assets currently include SHA-256 checksums but are not cryptographically signed.
:::

## Recent releases

### v0.2.14

Adds System, Wiretap, Nord, and Catppuccin themes and broadens bounded syntax highlighting while retaining large-payload safeguards.

### v0.2.13

Uses the Linux Secret Service default collection for client and relay-admin credentials while preserving the protected-file fallback for unattended clients.

### v0.2.12

Consolidates Wiretap secrets under one native keyring service and verifies relay-admin tokens immediately after saving.

### v0.2.11

Adds named relay-admin profiles and keyring-first storage for the desktop client token.

### v0.2.10

Reorganizes Settings around workflows and completes project creation, reassignment, deletion, and automatic tunnel synchronization.

### v0.2.9

Adds relay administration to the desktop and portable transform import, export, and duplication.

For detailed history, read the repository [changelog](https://github.com/plutack/wiretap/blob/main/CHANGELOG.md) or the notes attached to each GitHub release.
