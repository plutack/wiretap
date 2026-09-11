//go:build linux

package secretstore

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestCollectionNameFromPath(t *testing.T) {
	tests := []struct {
		name string
		path dbus.ObjectPath
		want string
	}{
		{name: "login", path: "/org/freedesktop/secrets/collection/login", want: "login"},
		{name: "escaped underscore", path: "/org/freedesktop/secrets/collection/Default_5fkeyring", want: "Default_keyring"},
		{name: "escaped UTF-8", path: "/org/freedesktop/secrets/collection/caf_c3_a9", want: "café"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := collectionNameFromPath(tt.path)
			if err != nil {
				t.Fatalf("collectionNameFromPath: %v", err)
			}
			if got != tt.want {
				t.Fatalf("collectionNameFromPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestCollectionNameFromPathRejectsInvalidPaths(t *testing.T) {
	for _, path := range []dbus.ObjectPath{
		"/",
		"/org/freedesktop/secrets/aliases/default",
		"/org/freedesktop/secrets/collection/",
		"/org/freedesktop/secrets/collection/bad_escape",
		"/org/freedesktop/secrets/collection/nested/path",
	} {
		if _, err := collectionNameFromPath(path); err == nil {
			t.Errorf("collectionNameFromPath(%q) succeeded", path)
		}
	}
}

func TestSystemRoundTripUsesDefaultCollectionWithoutCreatingOne(t *testing.T) {
	if os.Getenv("WIRETAP_KEYRING_INTEGRATION") == "" {
		t.Skip("set WIRETAP_KEYRING_INTEGRATION=1 to exercise the desktop keyring")
	}

	bus, err := dbus.SessionBus()
	if err != nil {
		t.Fatalf("connect to session bus: %v", err)
	}
	serviceObject := bus.Object(secretServiceName, secretServicePath)
	collectionsBefore := secretServiceCollections(t, serviceObject)

	var defaultPath dbus.ObjectPath
	if err := serviceObject.Call(secretServiceReadAlias, 0, "default").Store(&defaultPath); err != nil {
		t.Fatalf("resolve default alias: %v", err)
	}

	account := "integration-default/" + time.Now().UTC().Format("20060102T150405.000000000")
	store := System{}
	if err := store.Set(account, "round-trip-secret"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(account) })

	var matches []dbus.ObjectPath
	attributes := map[string]string{"profile": account}
	if err := bus.Object(secretServiceName, defaultPath).
		Call("org.freedesktop.Secret.Collection.SearchItems", 0, attributes).
		Store(&matches); err != nil {
		t.Fatalf("search default collection: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("default collection contains %d matching items, want 1", len(matches))
	}

	collectionsAfter := secretServiceCollections(t, serviceObject)
	if !slices.Equal(collectionsAfter, collectionsBefore) {
		t.Fatalf("keyring write changed collection inventory\nbefore: %v\n after: %v", collectionsBefore, collectionsAfter)
	}
}

func secretServiceCollections(t *testing.T, object dbus.BusObject) []dbus.ObjectPath {
	t.Helper()
	property, err := object.GetProperty("org.freedesktop.Secret.Service.Collections")
	if err != nil {
		t.Fatalf("list Secret Service collections: %v", err)
	}
	collections, ok := property.Value().([]dbus.ObjectPath)
	if !ok {
		t.Fatalf("unexpected collections property type %T", property.Value())
	}
	slices.Sort(collections)
	return slices.Clone(collections)
}
