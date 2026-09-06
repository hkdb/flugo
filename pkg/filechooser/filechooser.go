// Package filechooser provides a built-in file chooser service for Flugo apps.
// On Linux, it uses XDG Desktop Portal (via D-Bus) with a zenity/kdialog fallback.
// On other platforms, methods return an unsupported-platform error.
package filechooser

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/hkdb/flugo/pkg/bridge"
)

// WriteFileStream is the streaming counterpart of WriteFile: it selects the
// destination for the current environment (a temp file on Flatpak/mobile that
// the Dart layer then saves via the portal/SAF dialog, or the target path
// directly on native desktop), then streams the content produced by fn into it —
// no full-file buffer, so multi-GB outputs never materialize in RAM. On native
// desktop with force=false and an existing target it returns Exists without
// writing, exactly like WriteFile. writeTarget is provided per platform.
func WriteFileStream(targetPath string, force bool, fn func(w io.Writer) error) (WriteResult, error) {
	dest, env, exists, err := writeTarget(targetPath, force)
	if err != nil {
		return WriteResult{}, err
	}
	if exists {
		return WriteResult{Path: targetPath, Env: env, Exists: true}, nil
	}
	// 0600, not os.Create's 0644 — outputs can be decrypted plaintext, which
	// must not be world-readable on shared desktop systems.
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return WriteResult{}, fmt.Errorf("creating output: %w", err)
	}
	if perr := fn(f); perr != nil {
		f.Close()
		_ = os.Remove(dest)
		return WriteResult{}, perr
	}
	if cerr := f.Close(); cerr != nil {
		_ = os.Remove(dest)
		return WriteResult{}, fmt.Errorf("finalizing output: %w", cerr)
	}
	return WriteResult{Path: dest, Env: env}, nil
}

// tempOutputPath creates a unique temp subdirectory and returns the path for the output file.
// The subdirectory ensures no filename clashes between operations and easy cleanup.
func tempOutputPath(targetPath string) (string, error) {
	id := randomID()
	opDir := filepath.Join(bridge.TmpDir(), id)
	if err := os.MkdirAll(opDir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(opDir, filepath.Base(targetPath)), nil
}

// randomID generates a short random hex string for unique temp subdirectories.
// Falls back to a time-based ID if crypto/rand is unavailable (extremely rare —
// would mean the OS entropy source is broken).
func randomID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// FileChooserService is a Flugo bridge service providing file chooser dialogs.
// It is auto-registered via init() so apps get portal-compliant file dialogs
// with zero setup.
type FileChooserService struct{}

func init() {
	bridge.Bind(&FileChooserService{})
}

// WriteResult is returned by WriteFile with the path where data was written
// and the detected environment. The Dart-side handleWriteResult() uses this
// to show the appropriate platform dialog (portal on Flatpak, SAF on mobile).
type WriteResult struct {
	Path   string `json:"path"`             // where the file was written (or would be written)
	Env    string `json:"env"`              // "native", "flatpak", or "mobile"
	Exists bool   `json:"exists,omitempty"` // file already exists at targetPath (native only, when force=false)
}
