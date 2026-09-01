package bridge

import "sync"

// baseDir/tmpDir are set once by Flutter at startup but read from the
// Dart-owned FFI dispatch threads (and by other packages such as keyring), so
// access is guarded by pathsMu to avoid a data race with a lazy/late SetBaseDir.
var (
	pathsMu sync.RWMutex

	// baseDir holds the app's base directory set by Flutter at startup.
	// This is the platform-appropriate equivalent of $HOME for the app.
	// Developers build their own subdirectory structure within it.
	baseDir string

	// tmpDir holds the app's temporary directory set by Flutter at startup.
	// Safe for scratch files. Not inside share_plus's cache folder.
	tmpDir string
)

// flugoPathService is registered as a built-in bridge service.
type flugoPathService struct{}

// SetBaseDir is called by Flutter to set the app's base directory.
func (s *flugoPathService) SetBaseDir(dir string) (string, error) {
	SetBaseDir(dir)
	return "ok", nil
}

// SetTmpDir is called by Flutter to set the app's temp directory.
func (s *flugoPathService) SetTmpDir(dir string) (string, error) {
	SetTmpDir(dir)
	return "ok", nil
}

// SetBaseDir sets the app's base directory from Go code.
func SetBaseDir(dir string) {
	pathsMu.Lock()
	defer pathsMu.Unlock()
	baseDir = dir
}

// BaseDir returns the app's base directory.
// On all platforms, this is set by Flutter via path_provider at startup.
// Returns empty string if not yet set (e.g. running outside Flutter).
func BaseDir() string {
	pathsMu.RLock()
	defer pathsMu.RUnlock()
	return baseDir
}

// SetTmpDir sets the app's temp directory from Go code.
func SetTmpDir(dir string) {
	pathsMu.Lock()
	defer pathsMu.Unlock()
	tmpDir = dir
}

// TmpDir returns the app's temporary directory.
// Set by Flutter at startup. Safe for scratch files and sharing.
// Returns empty string if not yet set.
func TmpDir() string {
	pathsMu.RLock()
	defer pathsMu.RUnlock()
	return tmpDir
}

func init() {
	Bind(&flugoPathService{})
}
