package cli

import "testing"

func TestTitlebarModeFrameless(t *testing.T) {
	tests := []struct {
		mode string
		want bool
	}{
		{"auto", false},
		{"always", false},
		{"never", true},
		{"", false},
		{"  AUTO  ", false},
		{"  NEVER  ", true},
	}
	for _, tc := range tests {
		if got := titlebarModeFrameless(tc.mode); got != tc.want {
			t.Errorf("titlebarModeFrameless(%q) = %v, want %v", tc.mode, got, tc.want)
		}
	}
}
