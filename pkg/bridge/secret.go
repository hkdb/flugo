package bridge

import (
	"fmt"

	"github.com/awnumar/memguard"
)

// Secret is an opaque handle to bytes that crossed the FFI boundary via the
// raw-bytes secure entrypoint (FlugoCallSecure) instead of the standard JSON
// path. Bytes received this way are sealed into a memguard enclave on intake —
// wiping the intermediate Go copy in the process — and never get materialized
// as a Go string by the bridge layer. (The Dart-side source buffer is outside
// Go's reach and must be zeroed by the caller.)
//
// Use *Secret as a method parameter type when the bridge needs to receive
// sensitive material (passphrases, key material) from Dart without leaking
// plaintext copies through json.Unmarshal'd strings or jsonEncode'd Dart
// Strings. The Dart-side wrapper takes a Uint8List and invokes the secure
// path automatically.
//
// The bound method is responsible for opening the enclave for the minimum
// necessary scope (typically one Sign/Decrypt/derive call), destroying the
// returned LockedBuffer, and calling Destroy on the Secret when fully done
// with it. After Destroy the Secret is unusable.
type Secret struct {
	enclave *memguard.Enclave
}

// NewSecret wraps a byte slice into a sealed enclave. The input slice IS
// wiped by memguard.NewBufferFromBytes — do not retain or reuse it after
// passing it here. Returns an error only if memguard fails to allocate
// (extremely rare; usually indicates exhausted mlock budget).
func NewSecret(b []byte) (*Secret, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("secret: empty byte slice")
	}
	buf := memguard.NewBufferFromBytes(b)
	if buf == nil {
		return nil, fmt.Errorf("secret: memguard allocation failed")
	}
	return &Secret{enclave: buf.Seal()}, nil
}

// Open temporarily exposes the underlying bytes inside a memguard
// LockedBuffer. The caller MUST call Destroy on the returned buffer as soon
// as the operation completes (defer is the idiomatic pattern). The bytes
// are zeroed and unmapped immediately on Destroy.
func (s *Secret) Open() (*memguard.LockedBuffer, error) {
	if s == nil || s.enclave == nil {
		return nil, fmt.Errorf("secret: already destroyed")
	}
	return s.enclave.Open()
}

// Destroy wipes the underlying enclave. Idempotent — safe to call multiple
// times and on a nil receiver. After Destroy any further Open returns an
// error.
func (s *Secret) Destroy() {
	if s == nil || s.enclave == nil {
		return
	}
	if buf, err := s.enclave.Open(); err == nil {
		buf.Destroy()
	}
	s.enclave = nil
}
