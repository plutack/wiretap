//go:build gui

package guiassets

import (
	"embed"
	"io/fs"
)

//go:embed packaging/linux/wiretap.svg
var Icon []byte

// Assets is the embedded ./ui frontend tree served by the Wails webview,
// re-rooted at the "ui" directory so the Wails v3 asset server finds
// index.html at the root of the FS.
//
//go:embed all:ui
var uiFS embed.FS

// Assets never fails to build because "ui" is a compile-time embedded
// directory; fs.Sub can only fail on a bad name, so panic is appropriate.
var Assets fs.FS = func() fs.FS {
	sub, err := fs.Sub(uiFS, "ui")
	if err != nil {
		panic("guiassets: " + err.Error())
	}
	return sub
}()
