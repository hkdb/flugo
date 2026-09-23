//go:build linux && !android

package filechooser

import (
	"fmt"
	"os"
	"strings"

	"github.com/godbus/dbus/v5"
)

// PickFile shows a file picker dialog for selecting a single file.
// Returns the chosen path, or ("", nil) if the user canceled.
const portalOpenFile = "org.freedesktop.portal.FileChooser.OpenFile"

// portalFileChooser runs a FileChooser portal call (OpenFile/SaveFile/SaveFiles)
// and classifies the outcome, leaving each caller to keep its own failure policy
// (Pick*/SaveFile fall back to zenity/kdialog; SaveFiles errors instead). It
// injects handle_token into opts.
//
//   - portalFailed=true : the portal path is unusable (no session bus, portal
//     absent, or request/call/wait failure) — caller should fall back or error.
//   - canceled=true     : the user canceled (portal response 1).
//   - err != nil        : a real failure (other non-zero response, or no URIs).
//   - otherwise         : uris holds the selection.
func portalFileChooser(title, method string, opts map[string]dbus.Variant) (uris []string, canceled, portalFailed bool, err error) {
	conn, e := dbus.SessionBus()
	if e != nil {
		return nil, false, true, nil
	}
	if !portalAvailable(conn) {
		return nil, false, true, nil
	}
	req, e := newPortalRequest(conn)
	if e != nil {
		return nil, false, true, nil
	}
	defer req.cleanup()

	if opts == nil {
		opts = map[string]dbus.Variant{}
	}
	opts["handle_token"] = dbus.MakeVariant(req.handleToken())

	obj := conn.Object(portalDest, portalPath)
	if call := obj.Call(method, 0, "", title, opts); call.Err != nil {
		return nil, false, true, nil
	}
	response, results, e := req.wait()
	if e != nil {
		return nil, false, true, nil
	}
	if response == 1 {
		return nil, true, false, nil
	}
	if response != 0 {
		return nil, false, false, fmt.Errorf("file chooser failed (response: %d)", response)
	}
	u, ok := results["uris"].Value().([]string)
	if !ok || len(u) == 0 {
		return nil, false, false, fmt.Errorf("no URIs in portal response")
	}
	return u, false, false, nil
}

func (s *FileChooserService) PickFile(title string) (string, error) {
	uris, canceled, portalFailed, err := portalFileChooser(title, portalOpenFile, map[string]dbus.Variant{
		"multiple":  dbus.MakeVariant(false),
		"directory": dbus.MakeVariant(false),
	})
	if portalFailed {
		return fallbackPickFile(title)
	}
	if canceled {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return uriToPath(uris[0])
}

// PickFiles shows a file picker dialog for selecting multiple files.
// Returns the chosen paths, or (nil, nil) if the user canceled.
func (s *FileChooserService) PickFiles(title string) ([]string, error) {
	uris, canceled, portalFailed, err := portalFileChooser(title, portalOpenFile, map[string]dbus.Variant{
		"multiple":  dbus.MakeVariant(true),
		"directory": dbus.MakeVariant(false),
	})
	if portalFailed {
		return fallbackPickFiles(title)
	}
	if canceled {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return urisToPath(uris)
}

// PickDirectory shows a directory picker dialog.
// Returns the chosen path, or ("", nil) if the user canceled.
func (s *FileChooserService) PickDirectory(title string) (string, error) {
	uris, canceled, portalFailed, err := portalFileChooser(title, portalOpenFile, map[string]dbus.Variant{
		"multiple":  dbus.MakeVariant(false),
		"directory": dbus.MakeVariant(true),
	})
	if portalFailed {
		return fallbackPickDirectory(title)
	}
	if canceled {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return uriToPath(uris[0])
}

// writeTarget selects the write destination for the current Linux
// environment: a temp file under Flatpak (Dart saves it via the portal), or the
// target path on native desktop (Exists reported when force is false and the
// file already exists). WriteFile / WriteFileStream in filechooser.go do the
// writing.
func writeTarget(targetPath string, force bool) (dest, env string, exists bool, err error) {
	if isFlatpak() {
		tmpPath, terr := tempOutputPath(targetPath)
		if terr != nil {
			return "", "", false, fmt.Errorf("creating temp dir: %w", terr)
		}
		return tmpPath, "flatpak", false, nil
	}
	if !force {
		if _, serr := os.Stat(targetPath); serr == nil {
			return "", "native", true, nil
		}
	}
	return targetPath, "native", false, nil
}

// isFlatpak checks if the app is running inside a Flatpak sandbox.
func isFlatpak() bool {
	_, err := os.Stat("/.flatpak-info")
	return err == nil
}

// IsFlatpak reports whether the app is running inside a Flatpak sandbox.
func (s *FileChooserService) IsFlatpak() (bool, error) {
	return isFlatpak(), nil
}

// SaveFile shows a save file dialog.
// When targetPath is set and the app is NOT in a Flatpak sandbox, it returns
// targetPath immediately without showing a dialog. Inside Flatpak, targetPath
// is ignored and the portal dialog is shown (sandbox restrictions require it).
// Returns the chosen path, or ("", nil) if the user canceled.
func (s *FileChooserService) SaveFile(title, suggestedName, directory, targetPath string) (string, error) {
	if targetPath != "" && !isFlatpak() {
		return targetPath, nil
	}

	options := map[string]dbus.Variant{
		"current_name": dbus.MakeVariant(suggestedName),
	}
	if directory != "" {
		options["current_folder"] = dbus.MakeVariant(append([]byte(directory), 0))
	}

	uris, canceled, portalFailed, err := portalFileChooser(title, "org.freedesktop.portal.FileChooser.SaveFile", options)
	if portalFailed {
		return fallbackSaveFile(title, suggestedName, directory)
	}
	if canceled {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return uriToPath(uris[0])
}

// SaveFiles shows a save files dialog for multiple files.
// Returns the chosen paths, or (nil, nil) if the user canceled.
func (s *FileChooserService) SaveFiles(title string, filenames []string, directory string) ([]string, error) {
	files := make([][]byte, len(filenames))
	for i, name := range filenames {
		files[i] = append([]byte(name), 0)
	}

	options := map[string]dbus.Variant{
		"files": dbus.MakeVariant(files),
	}
	if directory != "" {
		options["current_folder"] = dbus.MakeVariant(append([]byte(directory), 0))
	}

	// SaveFiles has NO zenity/kdialog fallback — a portal failure is a real error.
	uris, canceled, portalFailed, err := portalFileChooser(title, "org.freedesktop.portal.FileChooser.SaveFiles", options)
	if portalFailed {
		return nil, fmt.Errorf("SaveFiles: file portal unavailable (no fallback)")
	}
	if canceled {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return urisToPath(uris)
}

// OpenFile opens a file with the default application using the OpenURI portal.
func (s *FileChooserService) OpenFile(filePath string) error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return fallbackOpen(filePath)
	}

	if !portalAvailable(conn) {
		return fallbackOpen(filePath)
	}

	f, err := os.OpenFile(filePath, os.O_RDWR, 0)
	if err != nil {
		f, err = os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open file %q: %w", filePath, err)
		}
	}
	defer f.Close()

	obj := conn.Object(portalDest, portalPath)
	options := map[string]dbus.Variant{
		"writable": dbus.MakeVariant(true),
	}
	call := obj.Call("org.freedesktop.portal.OpenURI.OpenFile", 0, "", dbus.UnixFD(f.Fd()), options)
	if call.Err != nil {
		return fallbackOpen(filePath)
	}

	return nil
}

// OpenDirectory opens the containing folder of a file using the OpenURI portal.
func (s *FileChooserService) OpenDirectory(filePath string) error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return fallbackOpen(filePath)
	}

	if !portalAvailable(conn) {
		return fallbackOpen(filePath)
	}

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file %q: %w", filePath, err)
	}
	defer f.Close()

	obj := conn.Object(portalDest, portalPath)
	options := map[string]dbus.Variant{}
	call := obj.Call("org.freedesktop.portal.OpenURI.OpenDirectory", 0, "", dbus.UnixFD(f.Fd()), options)
	if call.Err != nil {
		return fallbackOpen(filePath)
	}

	return nil
}

// OpenURL opens a URL (http, https, mailto, ...) with the user's default
// handler using the OpenURI portal, so it works inside Flatpak and other
// sandboxes. Falls back to xdg-open when no portal is available.
func (s *FileChooserService) OpenURL(url string) error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return fallbackOpen(url)
	}

	if !portalAvailable(conn) {
		return fallbackOpen(url)
	}

	obj := conn.Object(portalDest, portalPath)
	options := map[string]dbus.Variant{}
	call := obj.Call("org.freedesktop.portal.OpenURI.OpenURI", 0, "", url, options)
	if call.Err != nil {
		return fallbackOpen(url)
	}

	return nil
}

// IsDocPortalPath checks if a path is on the XDG document portal FUSE mount.
func (s *FileChooserService) IsDocPortalPath(path string) (bool, error) {
	return strings.HasPrefix(path, "/run/user/") && strings.Contains(path, "/doc/"), nil
}
