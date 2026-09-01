//go:build !android

package keyring

// unsupportedKeyring is returned on platforms where flugo's keyring is not available.
// Desktop platforms should use platform-specific keychain libraries directly.
type unsupportedKeyring struct{}

func newPlatformKeyring() Keyring {
	return &unsupportedKeyring{}
}

func (k *unsupportedKeyring) Available() bool                { return false }
func (k *unsupportedKeyring) Set(key, value string) error    { return ErrUnavailable }
func (k *unsupportedKeyring) Get(key string) (string, error) { return "", ErrUnavailable }
func (k *unsupportedKeyring) Delete(key string) error        { return ErrUnavailable }
func (k *unsupportedKeyring) List() ([]string, error)        { return nil, ErrUnavailable }

// LastError returns "" on non-Android platforms.
func LastError() string { return "" }
