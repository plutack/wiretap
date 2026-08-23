//go:build gui && !linux

package cli

import "context"

func applyGTKSystemTheme() {}

func watchGTKSystemTheme(context.Context, func()) {}
