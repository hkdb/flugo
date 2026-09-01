//go:build android

package filechooser

import (
	"fmt"
	"os"
)

// WriteFile writes data to a unique temp subdirectory on Android.
// The Dart side handles the SAF dialog for user-chosen save location.
func WriteFile(targetPath string, data []byte, force bool) (WriteResult, error) {
	tmpPath, err := tempOutputPath(targetPath)
	if err != nil {
		return WriteResult{}, fmt.Errorf("creating temp dir: %w", err)
	}
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return WriteResult{}, fmt.Errorf("writing temp file: %w", err)
	}
	return WriteResult{Path: tmpPath, Env: "mobile"}, nil
}

// writeTarget selects the streaming-write destination on Android: a temp file
// that the Dart side then saves to the user's chosen location via the SAF dialog.
func writeTarget(targetPath string, _ bool) (dest, env string, exists bool, err error) {
	tmpPath, terr := tempOutputPath(targetPath)
	if terr != nil {
		return "", "", false, fmt.Errorf("creating temp dir: %w", terr)
	}
	return tmpPath, "mobile", false, nil
}

// Stub service methods — on Android, Dart handles dialogs via Flutter plugins.

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
