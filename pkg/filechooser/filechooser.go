// Package filechooser provides a built-in file chooser service for Flugo apps.
// On Linux, it uses XDG Desktop Portal (via D-Bus) with a zenity/kdialog fallback.
// On other platforms, methods return an unsupported-platform error.
//
// It also owns the one correct way to write an output file for the current
// environment (WriteFile / WriteFileStream): on native desktop the write is
// atomic — content goes to a temp file beside the target and is renamed into
// place only once complete, so a crash never leaves a partial file and an
// overwrite never destroys the original before the replacement is whole. On
// Flatpak and mobile the content goes to a temp file that the Dart side then
// saves through the portal / SAF dialog. WriteFileStreamDeferred exposes the
// same flow with the final step held back, for callers that must inspect the
// result (verify a signature, check a digest) before committing it.
package filechooser

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hkdb/flugo/pkg/bridge"
)

// WriteResult is returned by WriteFile / WriteFileStream / CommitFile with the
// path where data was written and the detected environment. The Dart-side
// handleWriteResult() uses this to show the appropriate platform dialog
// (portal on Flatpak, SAF on mobile).
type WriteResult struct {
	Path   string `json:"path"`             // where the file was written (or would be written)
	Env    string `json:"env"`              // "native", "flatpak", or "mobile"
	Exists bool   `json:"exists,omitempty"` // file already exists at targetPath (native only, when force=false)
}

// PendingWrite is a completed but not yet committed write: the content is
// whole at TmpPath and nothing has touched TargetPath. It is JSON-encodable so
// a backend can hand it to the Dart side across a confirmation dialog and get
// it back for CommitFile or DiscardFile. Both re-validate the paths: a
// PendingWrite that came back altered can neither promote nor delete anything
// outside the temp file this package created.
type PendingWrite struct {
	TmpPath    string `json:"tmp_path"`
	TargetPath string `json:"target_path"`
	Env        string `json:"env"`              // "native", "flatpak", or "mobile"
	Exists     bool   `json:"exists,omitempty"` // nothing was written: target exists and force=false (native only)
}

// WriteFile writes data as WriteFileStream does — atomically on native
// desktop, to a temp file the Dart side saves on Flatpak/mobile. With
// force=false and an existing target on native desktop it returns Exists
// without writing, so the Dart side can ask the user for confirmation.
func WriteFile(targetPath string, data []byte, force bool) (WriteResult, error) {
	return WriteFileStream(targetPath, force, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// WriteFileStream streams the content produced by fn to the right destination
// for the current environment — no full-file buffer, so multi-GB outputs never
// materialize in RAM. On native desktop the write is atomic: fn writes to a
// temp file beside targetPath which is renamed into place only after fn
// returns successfully and the bytes are synced; if fn fails, targetPath is
// untouched (an existing file survives even with force=true). With force=false
// and an existing target it returns Exists without writing, exactly like
// WriteFile. writeTarget is provided per platform.
func WriteFileStream(targetPath string, force bool, fn func(w io.Writer) error) (WriteResult, error) {
	p, err := WriteFileStreamDeferred(targetPath, force, fn)
	if err != nil {
		return WriteResult{}, err
	}
	if p.Exists {
		return WriteResult{Path: targetPath, Env: p.Env, Exists: true}, nil
	}
	res, err := CommitFile(p, force)
	if err != nil {
		_ = DiscardFile(p)
		return WriteResult{}, err
	}
	return res, nil
}

// WriteFileStreamDeferred is WriteFileStream with the final step held back:
// it streams fn's output to a temp file and returns a PendingWrite instead of
// promoting it. Use it when the content must be inspected before it may
// appear at targetPath — then call CommitFile to promote it or DiscardFile to
// drop it. With force=false and an existing target on native desktop, fn is
// never called and the PendingWrite reports Exists. If fn fails or panics,
// the temp file is removed before the error (or panic) reaches the caller.
func WriteFileStreamDeferred(targetPath string, force bool, fn func(w io.Writer) error) (PendingWrite, error) {
	dest, env, exists, err := writeTarget(targetPath, force)
	if err != nil {
		return PendingWrite{}, err
	}
	if exists {
		return PendingWrite{TargetPath: targetPath, Env: env, Exists: true}, nil
	}
	f, err := openTemp(dest, env)
	if err != nil {
		return PendingWrite{}, fmt.Errorf("creating output: %w", err)
	}
	p := PendingWrite{TmpPath: f.Name(), TargetPath: targetPath, Env: env}
	committed := false
	defer func() {
		if committed {
			return
		}
		f.Close() // already closed on the success path; harmless here
		_ = DiscardFile(p)
	}()
	if perr := fn(f); perr != nil {
		return PendingWrite{}, perr
	}
	// Sync before the rename so a crash right after the commit cannot leave
	// a complete-looking file with unflushed contents.
	if serr := f.Sync(); serr != nil {
		return PendingWrite{}, fmt.Errorf("syncing output: %w", serr)
	}
	if cerr := f.Close(); cerr != nil {
		return PendingWrite{}, fmt.Errorf("finalizing output: %w", cerr)
	}
	committed = true
	return p, nil
}

// CommitFile promotes a pending write. On native desktop it renames the temp
// file into place (through an atomic copy when the rename is refused, e.g.
// across filesystems); with force=false and a target that has appeared since
// the write started it returns Exists and keeps the temp file for a retry.
// The check-then-rename window is the same one the previous direct write had
// (there is no portable no-replace rename); the caller has already asked the
// user by the time force is set. On Flatpak/mobile there is nothing to move —
// the Dart side saves the temp file through the portal/SAF dialog — so the
// result names the temp file, exactly as WriteFileStream always has.
func CommitFile(p PendingWrite, force bool) (WriteResult, error) {
	if p.Exists {
		return WriteResult{Path: p.TargetPath, Env: p.Env, Exists: true}, nil
	}
	if p.Env != "native" {
		return WriteResult{Path: p.TmpPath, Env: p.Env}, nil
	}
	if !isNativeTemp(p) {
		return WriteResult{}, fmt.Errorf("committing output: %q is not a temp file beside %q", p.TmpPath, p.TargetPath)
	}
	if !force {
		if _, err := os.Stat(p.TargetPath); err == nil {
			return WriteResult{Path: p.TargetPath, Env: "native", Exists: true}, nil
		}
	}
	if err := replaceFile(p.TmpPath, p.TargetPath); err != nil {
		return WriteResult{}, fmt.Errorf("committing output: %w", err)
	}
	syncDir(filepath.Dir(p.TargetPath))
	return WriteResult{Path: p.TargetPath, Env: "native"}, nil
}

// DiscardFile removes a pending write that will not be committed. It only
// ever deletes what this package created: on native desktop the hidden temp
// file beside the target, on Flatpak/mobile the per-operation directory under
// the app temp root (the same cleanup the Dart side's cleanupTemp performs).
// Anything else is refused with an error and left untouched.
func DiscardFile(p PendingWrite) error {
	if p.TmpPath == "" {
		return nil
	}
	if p.Env != "native" {
		dir, ok := ownedTempDir(p.TmpPath)
		if !ok {
			return fmt.Errorf("discarding output: refusing to remove %q: not inside the app temp directory", p.TmpPath)
		}
		return os.RemoveAll(dir)
	}
	if !isNativeTemp(p) {
		return fmt.Errorf("discarding output: refusing to remove %q: not a temp file beside %q", p.TmpPath, p.TargetPath)
	}
	if err := os.Remove(p.TmpPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// isNativeTemp reports whether p.TmpPath has the shape openTemp produces for
// p.TargetPath on native desktop: the same directory and a name of the form
// .<target base>.tmp-<random>.
func isNativeTemp(p PendingWrite) bool {
	tmp, target := filepath.Clean(p.TmpPath), filepath.Clean(p.TargetPath)
	if filepath.Dir(tmp) != filepath.Dir(target) {
		return false
	}
	prefix := "." + filepath.Base(target) + ".tmp-"
	base := filepath.Base(tmp)
	return strings.HasPrefix(base, prefix) && len(base) > len(prefix)
}

// ownedTempDir returns the per-operation temp directory that holds tmpPath —
// exactly <app temp root>/<id>/<name>, the shape tempOutputPath produces — or
// false for any other path.
func ownedTempDir(tmpPath string) (string, bool) {
	root := filepath.Clean(bridge.TmpDir())
	rel, err := filepath.Rel(root, filepath.Clean(tmpPath))
	if err != nil || filepath.IsAbs(rel) || rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return filepath.Join(root, parts[0]), true
}

// openTemp opens the file fn will write into. On native desktop that is a
// hidden temp file beside the target — same directory, so the commit rename
// is same-filesystem and atomic. Elsewhere dest already is the temp file the
// platform chose. Mode 0600 in both cases: outputs can be decrypted plaintext,
// which must not be world-readable on shared desktop systems.
func openTemp(dest, env string) (*os.File, error) {
	if env != "native" {
		return os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	}
	return os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".tmp-*")
}

// rename is os.Rename, swappable in tests to exercise replaceFile's fallback.
var rename = os.Rename

// replaceFile moves tmp to target, replacing any existing file. os.Rename is
// atomic on the same filesystem and replaces a symlink at target rather than
// following it. If it is refused (a cross-device link, say) the content is
// copied into a fresh temp file beside the target — never straight into
// target, which could be a symlink or be left half-written — synced, and that
// file is renamed into place; tmp is removed afterwards.
func replaceFile(tmp, target string) error {
	if err := rename(tmp, target); err == nil {
		return nil
	}
	src, err := os.Open(tmp)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := openTemp(target, "native")
	if err != nil {
		return err
	}
	moved := false
	defer func() {
		if moved {
			return
		}
		dst.Close()
		_ = os.Remove(dst.Name())
	}()
	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	if err := dst.Sync(); err != nil {
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	if err := os.Rename(dst.Name(), target); err != nil {
		return err
	}
	moved = true
	return os.Remove(tmp)
}

// syncDir flushes a directory so a rename into it is durable, not just the
// file data. Best-effort: some platforms and filesystems refuse to sync a
// directory handle, and a refusal changes nothing about the written bytes.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	d.Close()
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
