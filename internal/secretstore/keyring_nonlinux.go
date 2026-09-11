//go:build !linux

package secretstore

import "github.com/byteness/keyring"

func openSystemKeyring() (keyring.Keyring, error) {
	return keyring.Open(keyring.Config{
		ServiceName: service,
		AllowedBackends: []keyring.BackendType{
			keyring.KeychainBackend,
			keyring.WinCredBackend,
		},
	})
}
