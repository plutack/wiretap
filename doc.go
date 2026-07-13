// Package guiassets is the root-level home for the embedded Wails frontend
// assets. It exists as a package at the module root because Go's //go:embed
// directive cannot traverse parent directories (..), and the frontend lives in
// ./ui — so the embed must be declared by a file that is an ancestor of ./ui,
// i.e. a file in the repository root.
//
// The embed.FS itself is declared in guiassets.go, which is build-tagged `gui`
// so the default build (no GUI toolchain) never pulls the frontend or the Wails
// runtime into the dependency graph. This doc.go keeps the package present and
// importable even when the `gui` tag is off, so `go test ./...` and friends see
// a stable, empty-cost package rather than a "no Go files" directory.
package guiassets
