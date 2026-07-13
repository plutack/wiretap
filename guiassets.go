//go:build gui

package guiassets

import "embed"

// Assets is the embedded ./ui frontend tree served by the Wails webview. Wails
// auto-discovers index.html inside the FS and strips the leading "ui/" segment
// (see wails v2 pkg/assetserver/assethandler.go: FindPathToFile + iofs.Sub), so
// the raw embed.FS can be passed straight to options.App.Assets.
//
//go:embed all:ui
var Assets embed.FS
