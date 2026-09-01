package keyring

import "errors"

var (
	// ErrNotFound is returned when a key does not exist in the keyring.
	ErrNotFound = errors.New("keyring: key not found")
	// ErrUnavailable is returned when the keyring backend is not available.
	ErrUnavailable = errors.New("keyring: backend not available on this platform")
)
