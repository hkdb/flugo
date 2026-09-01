// Package keyring provides cross-platform secure key-value storage.
// On Android, it uses the Android Keystore via JNI. On other platforms,
// it reports as unavailable — desktop apps should use platform-specific
// keychain libraries (go-keyring, go-keychain) directly.
package keyring

// Keyring provides secure key-value storage backed by the platform's
// hardware-backed secure storage (e.g., Android Keystore).
type Keyring interface {
	// Available returns whether the keyring backend is functional.
	Available() bool

	// Set stores a value under the given key. Overwrites existing values.
	Set(key, value string) error

	// Get retrieves the value for the given key.
	// Returns ErrNotFound if the key does not exist.
	Get(key string) (string, error)

	// Delete removes the given key and its value.
	Delete(key string) error

	// List returns all stored key names.
	List() ([]string, error)
}

// New returns a Keyring implementation for the current platform.
// On Android, this uses the Android Keystore via JNI.
// On other platforms, the returned Keyring reports Available() == false.
func New() Keyring {
	return newPlatformKeyring()
}
