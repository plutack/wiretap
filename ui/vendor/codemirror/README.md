# Vendored CodeMirror 5

These files are the JavaScript editor used by the GUI script editor (PLAN.md
§6.1). They are vendored (committed to the repo) rather than loaded from a CDN
so the GUI works fully offline and has no runtime network dependency —
consistent with wiretap's no-node_modules, no-bundler frontend approach. The
whole tree is embedded into the binary via `//go:embed all:ui` (see
`guiassets.go`).

- Version: 5.65.16
- License: MIT (https://github.com/codemirror/codemirror5/blob/master/LICENSE)
- Source: https://cdnjs.cloudflare.com/ajax/libs/codemirror/5.65.16/

Files:

- `codemirror.js`  — core (minified UMD; exposes a global `CodeMirror`)
- `codemirror.css` — core styles
- `mode/javascript/javascript.js` — JS syntax-highlighting mode

CodeMirror 5 (not 6) is used deliberately: it ships as a handful of standalone
files loadable via plain `<script>`/`<link>` tags with a global, whereas
CodeMirror 6 is split across many npm packages and expects a bundler. To
upgrade, re-download the matching files from cdnjs and update the version above.
