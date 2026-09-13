# Vendored Microlighter

Wiretap vendors the small programmatic Microlighter runtime and only the
grammars used by captured HTTP bodies. This keeps the embedded Wails interface
offline-capable and avoids shipping languages that cannot be selected in the
body viewer.

- Version: 2.1.0
- License: MIT (see `LICENSE`)
- Source: https://github.com/davatron5000/microlighter
- Included grammars: CSS, GraphQL, HTML/XML, JavaScript, JSON, TOML, YAML

`ui/lib/syntax.js` feature-detects the CSS Custom Highlight API before loading
this module. Unsupported webviews retain Wiretap's existing JSON highlighter
and plain-text rendering.
