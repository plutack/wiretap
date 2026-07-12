package shellscript

import "errors"

// ErrUnsupportedShell is returned by Generate for a ShellKind the package
// doesn't know about. Callers use errors.Is to detect it.
var ErrUnsupportedShell = errors.New("shellscript: unsupported shell")
