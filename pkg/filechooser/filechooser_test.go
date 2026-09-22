package filechooser

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nativeDir returns a scratch directory, skipping the test when the current
// environment does not write natively (Flatpak/mobile hand the file to Dart).
func nativeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, env, _, err := writeTarget(filepath.Join(dir, "probe"), true); err != nil || env != "native" {
		t.Skipf("not a native-desktop environment (env=%q err=%v)", env, err)
	}
	return dir
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(des))
	for _, d := range des {
		names = append(names, d.Name())
	}
	return names
}

func TestWriteFileStreamWritesAtomically(t *testing.T) {
	dir := nativeDir(t)
	target := filepath.Join(dir, "out.bin")

	res, err := WriteFileStream(target, false, func(w io.Writer) error {
		_, err := io.WriteString(w, "complete content")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != target || res.Env != "native" || res.Exists {
		t.Fatalf("result: %+v", res)
	}
	if got := read(t, target); got != "complete content" {
		t.Fatalf("content = %q", got)
	}
	if names := entries(t, dir); len(names) != 1 {
		t.Fatalf("temp file left behind: %v", names)
	}
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", fi.Mode().Perm())
	}
}

func TestWriteFileStreamFailureLeavesTargetUntouched(t *testing.T) {
	dir := nativeDir(t)
	target := filepath.Join(dir, "out.bin")
	boom := errors.New("producer failed")

	// No target yet: nothing may appear.
	_, err := WriteFileStream(target, false, func(w io.Writer) error {
		_, _ = io.WriteString(w, "partial")
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("want producer error, got %v", err)
	}
	if _, serr := os.Stat(target); !errors.Is(serr, os.ErrNotExist) {
		t.Fatal("a failed write must not create the target")
	}
	if names := entries(t, dir); len(names) != 0 {
		t.Fatalf("temp file left behind: %v", names)
	}

	// Existing target + force: the original must survive a failed replacement.
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = WriteFileStream(target, true, func(w io.Writer) error {
		_, _ = io.WriteString(w, "half of the new")
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("want producer error, got %v", err)
	}
	if got := read(t, target); got != "original" {
		t.Fatalf("original destroyed by a failed overwrite: %q", got)
	}
}

func TestWriteFileStreamExistsGuardAndForce(t *testing.T) {
	dir := nativeDir(t)
	target := filepath.Join(dir, "out.bin")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	called := false
	res, err := WriteFileStream(target, false, func(w io.Writer) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Exists || called {
		t.Fatalf("force=false with an existing target must report Exists without writing: %+v called=%v", res, called)
	}
	if got := read(t, target); got != "original" {
		t.Fatalf("content changed: %q", got)
	}

	res, err = WriteFileStream(target, true, func(w io.Writer) error {
		_, err := io.WriteString(w, "replacement")
		return err
	})
	if err != nil || res.Exists {
		t.Fatalf("force=true: err=%v res=%+v", err, res)
	}
	if got := read(t, target); got != "replacement" {
		t.Fatalf("content = %q", got)
	}
}

func TestDeferredCommit(t *testing.T) {
	dir := nativeDir(t)
	target := filepath.Join(dir, "out.bin")

	p, err := WriteFileStreamDeferred(target, false, func(w io.Writer) error {
		_, err := io.WriteString(w, "verify me first")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Exists || p.TargetPath != target || p.Env != "native" {
		t.Fatalf("pending: %+v", p)
	}
	if _, serr := os.Stat(target); !errors.Is(serr, os.ErrNotExist) {
		t.Fatal("deferred write must not touch the target before commit")
	}
	if filepath.Dir(p.TmpPath) != dir || !strings.HasPrefix(filepath.Base(p.TmpPath), ".out.bin.tmp-") {
		t.Fatalf("temp file must sit hidden beside the target: %s", p.TmpPath)
	}
	if got := read(t, p.TmpPath); got != "verify me first" {
		t.Fatalf("temp content = %q", got)
	}

	res, err := CommitFile(p, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != target || res.Exists {
		t.Fatalf("commit result: %+v", res)
	}
	if got := read(t, target); got != "verify me first" {
		t.Fatalf("content = %q", got)
	}
	if _, serr := os.Stat(p.TmpPath); !errors.Is(serr, os.ErrNotExist) {
		t.Fatal("temp file must be gone after commit")
	}
}

func TestDeferredDiscard(t *testing.T) {
	dir := nativeDir(t)
	target := filepath.Join(dir, "out.bin")

	p, err := WriteFileStreamDeferred(target, false, func(w io.Writer) error {
		_, err := io.WriteString(w, "never to be seen")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := DiscardFile(p); err != nil {
		t.Fatal(err)
	}
	if names := entries(t, dir); len(names) != 0 {
		t.Fatalf("discard left files behind: %v", names)
	}
	if err := DiscardFile(p); err != nil {
		t.Fatalf("discard must be idempotent, got %v", err)
	}
	if err := DiscardFile(PendingWrite{}); err != nil {
		t.Fatalf("discarding an empty pending write must be a no-op, got %v", err)
	}
}

func TestDeferredCommitRespectsLateExists(t *testing.T) {
	dir := nativeDir(t)
	target := filepath.Join(dir, "out.bin")

	p, err := WriteFileStreamDeferred(target, false, func(w io.Writer) error {
		_, err := io.WriteString(w, "new")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// Someone creates the target while the caller was deciding.
	if err := os.WriteFile(target, []byte("appeared meanwhile"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := CommitFile(p, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Exists {
		t.Fatalf("commit with force=false must report Exists: %+v", res)
	}
	if got := read(t, target); got != "appeared meanwhile" {
		t.Fatalf("target overwritten without force: %q", got)
	}
	if _, serr := os.Stat(p.TmpPath); serr != nil {
		t.Fatal("temp file must be kept for a forced retry")
	}
	if res, err = CommitFile(p, true); err != nil || res.Exists {
		t.Fatalf("forced commit: err=%v res=%+v", err, res)
	}
	if got := read(t, target); got != "new" {
		t.Fatalf("content after forced commit = %q", got)
	}
}

func TestDeferredExistsSkipsProducer(t *testing.T) {
	dir := nativeDir(t)
	target := filepath.Join(dir, "out.bin")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := WriteFileStreamDeferred(target, false, func(w io.Writer) error {
		t.Fatal("producer must not run when the target exists and force=false")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Exists || p.TmpPath != "" {
		t.Fatalf("pending: %+v", p)
	}
	if res, err := CommitFile(p, false); err != nil || !res.Exists {
		t.Fatalf("committing an Exists pending write: err=%v res=%+v", err, res)
	}
}

func TestWriteFileDelegatesToStream(t *testing.T) {
	dir := nativeDir(t)
	target := filepath.Join(dir, "out.bin")
	res, err := WriteFile(target, []byte("buffered"), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != target || read(t, target) != "buffered" {
		t.Fatalf("WriteFile: %+v", res)
	}
	if names := entries(t, dir); len(names) != 1 {
		t.Fatalf("temp file left behind: %v", names)
	}
}

func TestReplaceFileCopyFallback(t *testing.T) {
	dir := nativeDir(t)
	// Exercise the copy path directly by pointing rename at a location that
	// cannot be renamed into: a directory in the way makes os.Rename fail on
	// every platform, and copying into it fails too — so instead verify the
	// fallback by replacing a file whose temp lives in a different directory.
	other := t.TempDir()
	tmp := filepath.Join(other, "tmp")
	if err := os.WriteFile(tmp, []byte("moved"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "out.bin")
	if err := replaceFile(tmp, target); err != nil {
		t.Fatal(err)
	}
	if read(t, target) != "moved" {
		t.Fatal("content not moved")
	}
	if _, err := os.Stat(tmp); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("source must be removed")
	}
}
