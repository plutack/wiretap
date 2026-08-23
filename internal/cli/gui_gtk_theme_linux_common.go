//go:build gui && linux

package cli

import (
	"context"

	"github.com/godbus/dbus/v5"
)

const (
	portalSettingsService = "org.freedesktop.portal.Desktop"
	portalSettingsPath    = "/org/freedesktop/portal/desktop"
	portalSettingsIface   = "org.freedesktop.portal.Settings"
	portalAppearance      = "org.freedesktop.appearance"
	portalColorScheme     = "color-scheme"
)

// applyGTKSystemTheme keeps GTK-owned window decorations consistent with the
// desktop-wide XDG portal preference. A missing/unknown preference falls back
// to dark because Wiretap's application chrome is dark.
func applyGTKSystemTheme() {
	setGTKDarkPreference(systemPrefersDark())
}

func systemPrefersDark() bool {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return true
	}
	defer conn.Close()

	var value dbus.Variant
	err = conn.Object(portalSettingsService, dbus.ObjectPath(portalSettingsPath)).
		Call(portalSettingsIface+".Read", 0, portalAppearance, portalColorScheme).
		Store(&value)
	if err != nil {
		return true
	}

	scheme, ok := value.Value().(uint32)
	if !ok {
		return true
	}
	return scheme != 2 // 2 means prefer light; 0 is no preference, 1 is dark.
}

func watchGTKSystemTheme(ctx context.Context, apply func()) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return
	}
	defer conn.Close()

	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath(dbus.ObjectPath(portalSettingsPath)),
		dbus.WithMatchInterface(portalSettingsIface),
		dbus.WithMatchMember("SettingChanged"),
	); err != nil {
		return
	}

	signals := make(chan *dbus.Signal, 4)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)

	for {
		select {
		case <-ctx.Done():
			return
		case signal, ok := <-signals:
			if !ok {
				return
			}
			if isPortalColorSchemeSignal(signal) {
				apply()
			}
		}
	}
}

func isPortalColorSchemeSignal(signal *dbus.Signal) bool {
	if signal == nil || len(signal.Body) < 2 {
		return false
	}
	namespace, namespaceOK := signal.Body[0].(string)
	key, keyOK := signal.Body[1].(string)
	return namespaceOK && keyOK && namespace == portalAppearance && key == portalColorScheme
}
