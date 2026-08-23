package cli

import "testing"

func TestNativeTitlebarWanted(t *testing.T) {
	tests := []struct {
		mode     string
		desktop  string
		want     bool
		explains string
	}{
		{"auto", "COSMIC", true, "COSMIC draws server-side bars"},
		{"auto", "X-COSMIC", true, "vendored prefix"},
		{"auto", "cosmic", true, "case-insensitive"},
		{"auto", "KDE", true, "Plasma draws server-side bars"},
		{"auto", "GNOME", false, "GNOME would leave the window barless"},
		{"auto", "sway", false, "wlroots compositors don't decorate"},
		{"auto", "", false, "no desktop info"},
		{"auto", "GNOME:KDE", true, "colon-separated list matches any"},
		{"always", "GNOME", true, "explicit override"},
		{"never", "COSMIC", false, "explicit override"},
		{"always", "", true, "explicit override"},
		{"", "COSMIC", true, "empty mode means auto"},
		{"  AUTO  ", "cosmic", true, "whitespace tolerated"},
	}
	for _, tc := range tests {
		if got := nativeTitlebarWanted(tc.mode, tc.desktop); got != tc.want {
			t.Errorf("nativeTitlebarWanted(%q, %q) = %v, want %v (%s)",
				tc.mode, tc.desktop, got, tc.want, tc.explains)
		}
	}
}
