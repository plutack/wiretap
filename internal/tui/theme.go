package tui

import "github.com/charmbracelet/lipgloss"

// theme is the palette the dashboard renders with. It finally gives the
// `tui.theme` config field (internal/config/config.go, editable from the GUI
// settings screen) an effect in the TUI itself, which previously hard-coded
// two colors. Names match the GUI's THEME_OPTIONS: "dark" and "light".
type theme struct {
	name    string
	title   lipgloss.Style // app title in the header
	dim     lipgloss.Style // secondary text: status line, footer, table rules
	accent  lipgloss.Style // tab labels, section headings
	error_  lipgloss.Style
	success lipgloss.Style
	warn    lipgloss.Style
	cursor  lipgloss.Style // selected row background
	method  lipgloss.Style // method badge
	badge   lipgloss.Style // generic badge (seq, sizes)
	// status family colors (traffic tab): 2xx/3xx/4xx/5xx
	status2xx lipgloss.Style
	status3xx lipgloss.Style
	status4xx lipgloss.Style
	status5xx lipgloss.Style
}

// themeFor resolves a config theme name, falling back to dark. The dark
// palette keeps the two colors the original dashboard used (63 for the title,
// 245 for the status line) so existing users see no regression.
func themeFor(name string) theme {
	if name == "light" {
		return lightTheme()
	}
	return darkTheme()
}

func darkTheme() theme {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	accent := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	return theme{
		name:      "dark",
		title:     title,
		dim:       dim,
		accent:    accent,
		error_:    lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
		success:   lipgloss.NewStyle().Foreground(lipgloss.Color("114")),
		warn:      lipgloss.NewStyle().Foreground(lipgloss.Color("179")),
		cursor:    lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("236")),
		method:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")),
		badge:     lipgloss.NewStyle().Foreground(lipgloss.Color("147")),
		status2xx: lipgloss.NewStyle().Foreground(lipgloss.Color("114")),
		status3xx: lipgloss.NewStyle().Foreground(lipgloss.Color("80")),
		status4xx: lipgloss.NewStyle().Foreground(lipgloss.Color("179")),
		status5xx: lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
	}
}

func lightTheme() theme {
	return theme{
		name:      "light",
		title:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("25")),
		dim:       lipgloss.NewStyle().Foreground(lipgloss.Color("242")),
		accent:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("31")),
		error_:    lipgloss.NewStyle().Foreground(lipgloss.Color("124")),
		success:   lipgloss.NewStyle().Foreground(lipgloss.Color("28")),
		warn:      lipgloss.NewStyle().Foreground(lipgloss.Color("130")),
		cursor:    lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("252")),
		method:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("31")),
		badge:     lipgloss.NewStyle().Foreground(lipgloss.Color("61")),
		status2xx: lipgloss.NewStyle().Foreground(lipgloss.Color("28")),
		status3xx: lipgloss.NewStyle().Foreground(lipgloss.Color("30")),
		status4xx: lipgloss.NewStyle().Foreground(lipgloss.Color("130")),
		status5xx: lipgloss.NewStyle().Foreground(lipgloss.Color("124")),
	}
}

// statusStyle picks the palette entry for one HTTP status family.
func (t theme) statusStyle(status int) lipgloss.Style {
	switch {
	case status >= 200 && status < 300:
		return t.status2xx
	case status >= 300 && status < 400:
		return t.status3xx
	case status >= 400 && status < 500:
		return t.status4xx
	case status >= 500:
		return t.status5xx
	}
	return t.dim
}
