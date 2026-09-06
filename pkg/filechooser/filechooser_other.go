//go:build !linux

package filechooser

import (
	"fmt"
	"os"
	"runtime"
)

// WriteFile writes data to the appropriate location based on the environment.
// On macOS/Windows, it writes directly to targetPath.
// WriteFile writes data to the appropriate location.
// On iOS, it writes to a temp directory (the Dart side handles the SAF dialog).
// On macOS/Windows, it writes directly to targetPath.
// When force is false and the file already exists on native desktop, it returns Exists: true
// without writing, so the Dart side can ask the user for confirmation.
func WriteFile(targetPath string, data []byte, force bool) (WriteResult, error) {
	// iOS: mobile
	if runtime.GOOS == "ios" {
		tmpPath, err := tempOutputPath(targetPath)
		if err != nil {
			return WriteResult{}, fmt.Errorf("creating temp dir: %w", err)
		}
		if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
			return WriteResult{}, fmt.Errorf("writing temp file: %w", err)
		}
		return WriteResult{Path: tmpPath, Env: "mobile"}, nil
	}
	// macOS, Windows: native desktop — check for existing file
	if !force {
		if _, err := os.Stat(targetPath); err == nil {
			return WriteResult{Path: targetPath, Env: "native", Exists: true}, nil
		}
	}
	if err := os.WriteFile(targetPath, data, 0o600); err != nil {
		return WriteResult{}, fmt.Errorf("writing file: %w", err)
	}
	return WriteResult{Path: targetPath, Env: "native"}, nil
}

// writeTarget selects the streaming-write destination: a temp file on iOS (Dart
// saves it via the SAF dialog), or the target path on macOS/Windows native
// desktop (Exists reported when force is false and the file already exists).
func writeTarget(targetPath string, force bool) (dest, env string, exists bool, err error) {
	if runtime.GOOS == "ios" {
		tmpPath, terr := tempOutputPath(targetPath)
		if terr != nil {
			return "", "", false, fmt.Errorf("creating temp dir: %w", terr)
		}
		return tmpPath, "mobile", false, nil
	}
	if !force {
		if _, serr := os.Stat(targetPath); serr == nil {
			return "", "native", true, nil
		}
	}
	return targetPath, "native", false, nil
}

func (s *FileChooserService) PickFile(title string) (string, error) {
	return "", fmt.Errorf("file chooser not supported on this platform")
}

func (s *FileChooserService) PickFiles(title string) ([]string, error) {
	return nil, fmt.Errorf("file chooser not supported on this platform")
}

func (s *FileChooserService) PickDirectory(title string) (string, error) {
	return "", fmt.Errorf("file chooser not supported on this platform")
}

func (s *FileChooserService) IsFlatpak() (bool, error) {
	return false, nil
}

func (s *FileChooserService) SaveFile(title, suggestedName, directory, targetPath string) (string, error) {
	if targetPath != "" {
		return targetPath, nil
	}
	return "", fmt.Errorf("file chooser not supported on this platform")
}

func (s *FileChooserService) SaveFiles(title string, filenames []string, directory string) ([]string, error) {
	return nil, fmt.Errorf("file chooser not supported on this platform")
}

func (s *FileChooserService) OpenFile(filePath string) error {
	return fmt.Errorf("file chooser not supported on this platform")
}

func (s *FileChooserService) OpenDirectory(filePath string) error {
	return fmt.Errorf("file chooser not supported on this platform")
}

func (s *FileChooserService) OpenURL(url string) error {
	return fmt.Errorf("URL opening not supported on this platform — the Dart layer uses url_launcher here")
}

func (s *FileChooserService) IsDocPortalPath(path string) (bool, error) {
	return false, nil
}
