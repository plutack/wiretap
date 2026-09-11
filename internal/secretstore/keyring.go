// Package secretstore provides the minimal credential-store surface used by
// relay administration and the local relay client.
package secretstore

import (
	"errors"
	"fmt"

	"github.com/byteness/keyring"
)

const service = "wiretap"

var ErrNotFound = errors.New("secret not found")

type Store interface {
	Set(account, secret string) error
	Get(account string) (string, error)
	Delete(account string) error
}

// System stores relay-admin tokens under the shared Wiretap service.
type System struct{}

// ClientSystem uses the same service. Unique account keys keep client and
// admin tokens independent without creating additional keyring collections.
type ClientSystem struct{}

func set(account, secret string) error {
	ring, err := openSystemKeyring()
	if err != nil {
		return fmt.Errorf("open system keyring: %w", err)
	}
	if err := ring.Set(secretItem(account, secret)); err != nil {
		return fmt.Errorf("set system keyring secret: %w", err)
	}
	return nil
}

func secretItem(account, secret string) keyring.Item {
	return keyring.Item{
		Key:         account,
		Data:        []byte(secret),
		Label:       "Wiretap credential",
		Description: "Credential stored securely by Wiretap",
	}
}

func get(account string) (string, error) {
	ring, err := openSystemKeyring()
	if err != nil {
		return "", fmt.Errorf("open system keyring: %w", err)
	}
	item, err := ring.Get(account)
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get system keyring secret: %w", err)
	}
	return string(item.Data), nil
}

func remove(account string) error {
	ring, err := openSystemKeyring()
	if err != nil {
		return fmt.Errorf("open system keyring: %w", err)
	}
	if err := ring.Remove(account); errors.Is(err, keyring.ErrKeyNotFound) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("delete system keyring secret: %w", err)
	}
	return nil
}

func (System) Set(account, secret string) error       { return set(account, secret) }
func (System) Get(account string) (string, error)     { return get(account) }
func (System) Delete(account string) error            { return remove(account) }
func (ClientSystem) Set(account, secret string) error { return set(account, secret) }
func (ClientSystem) Get(account string) (string, error) {
	return get(account)
}
func (ClientSystem) Delete(account string) error { return remove(account) }
