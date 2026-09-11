//go:build linux

package secretstore

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/byteness/keyring"
	"github.com/godbus/dbus/v5"
)

const (
	secretServiceName             = "org.freedesktop.secrets"
	secretServicePath             = dbus.ObjectPath("/org/freedesktop/secrets")
	secretServiceCollectionPrefix = "/org/freedesktop/secrets/collection/"
	secretServiceReadAlias        = "org.freedesktop.Secret.Service.ReadAlias"
)

func openSystemKeyring() (keyring.Keyring, error) {
	collection, err := defaultSecretServiceCollectionName()
	if err != nil {
		return nil, err
	}
	return keyring.Open(keyring.Config{
		ServiceName:             service,
		LibSecretCollectionName: collection,
		AllowedBackends: []keyring.BackendType{
			keyring.SecretServiceBackend,
		},
	})
}

// defaultSecretServiceCollectionName follows the standard Secret Service
// "default" alias. ByteNess accepts only a concrete collection name, so we
// resolve the alias before opening it instead of creating an app collection.
func defaultSecretServiceCollectionName() (string, error) {
	bus, err := dbus.SessionBus()
	if err != nil {
		return "", fmt.Errorf("connect to Secret Service session bus: %w", err)
	}

	var path dbus.ObjectPath
	call := bus.Object(secretServiceName, secretServicePath).
		Call(secretServiceReadAlias, 0, "default")
	if err := call.Store(&path); err != nil {
		return "", fmt.Errorf("resolve Secret Service default collection: %w", err)
	}
	return collectionNameFromPath(path)
}

func collectionNameFromPath(path dbus.ObjectPath) (string, error) {
	value := string(path)
	if path == "/" || !strings.HasPrefix(value, secretServiceCollectionPrefix) {
		return "", fmt.Errorf("Secret Service default alias returned invalid collection path %q", value)
	}

	encoded := strings.TrimPrefix(value, secretServiceCollectionPrefix)
	if encoded == "" || strings.Contains(encoded, "/") {
		return "", fmt.Errorf("Secret Service default alias returned invalid collection path %q", value)
	}

	var decoded strings.Builder
	for i := 0; i < len(encoded); i++ {
		if encoded[i] != '_' {
			decoded.WriteByte(encoded[i])
			continue
		}
		if i+2 >= len(encoded) {
			return "", fmt.Errorf("Secret Service collection path contains an invalid escape: %q", value)
		}
		b, err := strconv.ParseUint(encoded[i+1:i+3], 16, 8)
		if err != nil {
			return "", fmt.Errorf("Secret Service collection path contains an invalid escape: %q", value)
		}
		decoded.WriteByte(byte(b))
		i += 2
	}
	if decoded.Len() == 0 {
		return "", fmt.Errorf("Secret Service default alias returned an empty collection name")
	}
	return decoded.String(), nil
}
