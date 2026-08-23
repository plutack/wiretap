//go:build gui && linux && gtk3

package cli

/*
#cgo pkg-config: gtk+-3.0
#include <gtk/gtk.h>

static void wiretap_set_dark_theme(gboolean prefer_dark) {
	GtkSettings *settings = gtk_settings_get_default();
	if (settings != NULL) {
		g_object_set(settings, "gtk-application-prefer-dark-theme", prefer_dark, NULL);
	}
}
*/
import "C"

func setGTKDarkPreference(preferDark bool) {
	value := C.gboolean(0)
	if preferDark {
		value = C.gboolean(1)
	}
	C.wiretap_set_dark_theme(value)
}
