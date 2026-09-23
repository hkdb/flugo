//go:build !linux

package filechooser

import (
	"fmt"
	"os"
	"runtime"
)

// writeTarget selects the write destination: a temp file on iOS (Dart saves
// it via the SAF dialog), or the target path on macOS/Windows native desktop
// (Exists reported when force is false and the file already exists). WriteFile
// / WriteFileStream in filechooser.go do the writing.
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
