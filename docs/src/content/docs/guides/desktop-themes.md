---
title: Customize the desktop
description: Choose a Wiretap application theme, text size, row density, and title-bar behavior.
---

Open **Settings → Interface** to change the desktop without restarting it.

## Application themes

Wiretap includes:

- **System**, which follows the operating system's light or dark appearance;
- **Wiretap Dark** and **Wiretap Light**;
- **Nord**;
- **Catppuccin Mocha** and **Catppuccin Latte**.

The chosen palette applies to navigation, panels, form controls, the transform editor, and supported syntax highlighting. It is stored as a device-local preference rather than in `config.yaml`.

## Density and text size

Use row density to make webhook and traffic lists more compact or more scannable. Text size changes the workbench without changing the stored payload.

## Native title bar

The `gui.native_titlebar` configuration accepts:

- `auto` — keep the platform title bar;
- `always` — explicitly keep it;
- `never` — request a frameless Linux window.

Windows and macOS retain native window controls even when `never` is selected.

## Payload highlighting

Small complete JSON, JavaScript, HTML/XML, CSS, YAML, TOML, and GraphQL bodies use syntax highlighting when the webview supports it. Formatting and highlighting stop at 100 KiB.

Base64 and hex data runs of 2 KiB or more are collapsed into a chip that names the value and its size, with a thumbnail when the data is a recognised image. The surrounding body stays readable in context — a JSON envelope shows its keys, and only the blob becomes a chip. Clicking a chip renders that value inline; clicking it again collapses it. This keeps a multi-megabyte payload from freezing the interface without hiding its shape.

Large JSON still pretty-prints at any size, because the indented structure is what makes a collapsed value readable; only syntax highlighting stops at the 100 KiB limit.
