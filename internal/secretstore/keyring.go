// Package secretstore provides the minimal credential-store surface used by
// relay administration. Production delegates to the operating system keyring.
package secretstore

import (
	"errors"
	"fmt"

	"github.com/99designs/keyring"
)

const adminService = "wiretap relay admin"
const clientService = "wiretap relay client"

var ErrNotFound = errors.New("secret not found")

type Store interface {
	Set(account, secret string) error
	Get(account string) (string, error)
	Delete(account string) error
}

type System struct{}

// ClientSystem stores the long-lived token used by this desktop's relay
// tunnel. It deliberately uses a separate keyring service from relay admin
// profiles so revoking one kind of access cannot disturb the other.
type ClientSystem struct{}

func openSystem(serviceName, passPrefix, winCredPrefix string) (keyring.Keyring, error) {
	return keyring.Open(keyring.Config{
		ServiceName: serviceName,
		AllowedBackends: []keyring.BackendType{
			keyring.KeychainBackend,
			keyring.SecretServiceBackend,
			keyring.KWalletBackend,
			keyring.PassBackend,
			keyring.WinCredBackend,
		},
		KeychainAccessibleWhenUnlocked: true,
		KeychainTrustApplication:       true,
		KWalletAppID:                   "wiretap",
		KWalletFolder:                  "wiretap",
		PassPrefix:                     passPrefix,
		WinCredPrefix:                  winCredPrefix,
	})
}

func openAdminSystem() (keyring.Keyring, error) {
	return openSystem(adminService, "wiretap/relay-admin", "wiretap-relay-admin/")
}

func openClientSystem() (keyring.Keyring, error) {
	return openSystem(clientService, "wiretap/relay-client", "wiretap-relay-client/")
}

func (System) Set(account, secret string) error {
	ring, err := openAdminSystem()
	if err != nil {
		return fmt.Errorf("open system keyring: %w", err)
	}
	return ring.Set(keyring.Item{Key: account, Data: []byte(secret), Label: "Wiretap relay admin token"})
}
func (System) Get(account string) (string, error) {
	ring, err := openAdminSystem()
	if err != nil {
		return "", fmt.Errorf("open system keyring: %w", err)
	}
	item, err := ring.Get(account)
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return string(item.Data), nil
}
func (System) Delete(account string) error {
	ring, err := openAdminSystem()
	if err != nil {
		return fmt.Errorf("open system keyring: %w", err)
	}
	err = ring.Remove(account)
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return ErrNotFound
	}
	return err
}

func (ClientSystem) Set(account, secret string) error {
	ring, err := openClientSystem()
	if err != nil {
		return fmt.Errorf("open system keyring: %w", err)
	}
	return ring.Set(keyring.Item{Key: account, Data: []byte(secret), Label: "Wiretap relay client token"})
}

func (ClientSystem) Get(account string) (string, error) {
	ring, err := openClientSystem()
	if err != nil {
		return "", fmt.Errorf("open system keyring: %w", err)
	}
	item, err := ring.Get(account)
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return string(item.Data), nil
}

func (ClientSystem) Delete(account string) error {
	ring, err := openClientSystem()
	if err != nil {
		return fmt.Errorf("open system keyring: %w", err)
	}
	err = ring.Remove(account)
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return ErrNotFound
	}
	return err
}
