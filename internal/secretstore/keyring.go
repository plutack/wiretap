// Package secretstore provides the minimal credential-store surface used by
// relay administration. Production delegates to the operating system keyring.
package secretstore

import (
	"errors"
	"fmt"

	"github.com/99designs/keyring"
)

const service = "wiretap relay admin"

var ErrNotFound = errors.New("secret not found")

type Store interface {
	Set(account, secret string) error
	Get(account string) (string, error)
	Delete(account string) error
}

type System struct{}

func openSystem() (keyring.Keyring, error) {
	return keyring.Open(keyring.Config{
		ServiceName: service,
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
		PassPrefix:                     "wiretap/relay-admin",
		WinCredPrefix:                  "wiretap-relay-admin/",
	})
}

func (System) Set(account, secret string) error {
	ring, err := openSystem()
	if err != nil {
		return fmt.Errorf("open system keyring: %w", err)
	}
	return ring.Set(keyring.Item{Key: account, Data: []byte(secret), Label: "Wiretap relay admin token"})
}
func (System) Get(account string) (string, error) {
	ring, err := openSystem()
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
	ring, err := openSystem()
	if err != nil {
		return fmt.Errorf("open system keyring: %w", err)
	}
	err = ring.Remove(account)
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return ErrNotFound
	}
	return err
}
