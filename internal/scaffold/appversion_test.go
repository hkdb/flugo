package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const pubspecFixture = "name: demo\ndescription: A demo\npublish_to: 'none'\n\nversion: 0.1.2+7  # keep me\n\nenvironment:\n  sdk: ^3.11.1\n\ndependencies:\n  flutter:\n    sdk: flutter\n"

func projectWithPubspec(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "frontend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pubspecPath(dir), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSyncAppVersionRewritesOnlyTheBase(t *testing.T) {
	dir := projectWithPubspec(t, pubspecFixture)
	changed, err := SyncAppVersion(dir, "0.1.3", false)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	got, _ := os.ReadFile(pubspecPath(dir))
	want := strings.Replace(pubspecFixture, "version: 0.1.2+7  # keep me", "version: 0.1.3+7  # keep me", 1)
	if string(got) != want {
		t.Fatalf("pubspec after sync:\n%s\nwant:\n%s", got, want)
	}
	if v, _ := PubspecVersion(dir); v != "0.1.3" {
		t.Fatalf("PubspecVersion = %q", v)
	}
	// Idempotent.
	if changed, err := SyncAppVersion(dir, "0.1.3", false); err != nil || changed {
		t.Fatalf("second sync must be a no-op: changed=%v err=%v", changed, err)
	}
}

func TestSyncAppVersionDryRunAndEmpty(t *testing.T) {
	dir := projectWithPubspec(t, pubspecFixture)
	changed, err := SyncAppVersion(dir, "9.9.9", true)
	if err != nil || !changed {
		t.Fatalf("dry run must report a change: changed=%v err=%v", changed, err)
	}
	got, _ := os.ReadFile(pubspecPath(dir))
	if string(got) != pubspecFixture {
		t.Fatal("dry run must not write")
	}
	if changed, err := SyncAppVersion(dir, "", false); err != nil || changed {
		t.Fatalf("empty version must be a no-op: changed=%v err=%v", changed, err)
	}
}

func TestCheckAppVersion(t *testing.T) {
	dir := projectWithPubspec(t, pubspecFixture)
	if err := CheckAppVersion(dir, "0.1.2"); err != nil {
		t.Fatalf("matching base must pass (build suffix ignored): %v", err)
	}
	err := CheckAppVersion(dir, "0.1.3")
	if err == nil || !strings.Contains(err.Error(), "flugo gen") {
		t.Fatalf("stale pubspec must fail and point at flugo gen, got %v", err)
	}
	if err := CheckAppVersion(dir, ""); err != nil {
		t.Fatalf("no configured version must pass: %v", err)
	}
	if err := CheckAppVersion(t.TempDir(), "1.0.0"); err != nil {
		t.Fatalf("no pubspec must pass: %v", err)
	}
}
