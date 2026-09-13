# Desktop theming

Wiretap themes are palette presets over one semantic design system. A preset
changes the entire desktop workbench, including navigation, panels, form
controls, status colours, the transform editor, and read-only payload syntax.
It does not replace component markup or alter layout.

Open **Settings > Interface > Application theme** to choose:

- **System** follows the operating system's light or dark appearance.
- **Wiretap Dark** preserves the original charcoal and mint palette.
- **Wiretap Light** provides a low-glare light workbench.
- **Nord** applies the Nord arctic palette.
- **Catppuccin Mocha** applies Catppuccin's dark palette.
- **Catppuccin Latte** applies Catppuccin's light palette.

The selection is applied immediately and stored locally with the existing text
size and row-density preferences. It is not written to `config.yaml`, so each
desktop can use its own appearance.

## How presets work

`ui/layout.css` defines application tokens such as `--wt-bg`, `--wt-panel`,
`--wt-text`, `--wt-accent`, and the `--wt-code-*` syntax colours. Theme
selectors assign a palette to those tokens through `data-theme` on the root
HTML element.

`ui/input.css` maps Tailwind's colour utilities to the same tokens. Existing
utility-based components therefore change with the rest of the application
instead of retaining a fixed dark palette. CodeMirror and Microlighter also
consume the shared syntax tokens.

To add another preset:

1. Add its metadata and three preview colours to `THEMES` in
   `ui/lib/prefs.js`.
2. Add a matching `:root[data-theme="..."]` token block in `ui/layout.css`.
3. Add the identifier to the early theme bootstrap allow-list in
   `ui/index.html`.
4. Run `npm run css`, then inspect the workbench, settings, an editor, and a
   captured body in both normal and compact density.

Theme names such as Nord and Catppuccin identify palette families. Wiretap owns
the component styling and maps those palette colours onto its own semantic
roles.

## Payload syntax highlighting

Small, complete JSON, JavaScript, HTML/XML, CSS, YAML, TOML, and GraphQL bodies
use the vendored Microlighter 2.1.0 runtime when the webview supports the CSS
Custom Highlight API. Microlighter keeps the code as one text node and stores
token ranges outside the DOM. Older webviews retain the existing JSON span
highlighter or plain text.

This does not remove Wiretap's large-payload guard. TextMate scanning still
runs on the UI thread and becomes expensive on multi-megabyte structured JSON.
Pretty formatting and syntax highlighting therefore remain limited to complete
bodies no larger than 100 KiB. Larger captures use the existing bounded,
progressive plain-text preview.
