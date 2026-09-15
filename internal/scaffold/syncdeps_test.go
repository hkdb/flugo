package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBumpGoModFlugoRequire_BlockForm(t *testing.T) {
	in := `module example/backend

go 1.26.1

require (
	github.com/hkdb/flugo v0.1.13
	github.com/some/other v1.2.3
)
`
	out, changed := bumpGoModFlugoRequire([]byte(in), "v0.2.0")
	if !changed {
		t.Fatal("expected changed = true")
	}
	s := string(out)
	if !strings.Contains(s, "github.com/hkdb/flugo v0.2.0") {
		t.Errorf("flugo require not bumped:\n%s", s)
	}
	if !strings.Contains(s, "github.com/some/other v1.2.3") {
		t.Errorf("unrelated require was altered:\n%s", s)
	}
}

func TestBumpGoModFlugoRequire_SingleLineAndIndirect(t *testing.T) {
	in := "module example/backend\n\ngo 1.26.1\n\nrequire github.com/hkdb/flugo v0.1.13 // indirect\n"
	out, changed := bumpGoModFlugoRequire([]byte(in), "v0.2.0")
	if !changed {
		t.Fatal("expected changed = true")
	}
	s := string(out)
	if !strings.Contains(s, "require github.com/hkdb/flugo v0.2.0 // indirect") {
		t.Errorf("single-line require or trailing comment not preserved:\n%s", s)
	}
}

func TestBumpGoModFlugoRequire_DoesNotTouchReplaceLine(t *testing.T) {
	// A replace line must be left intact by the regex (real skip happens earlier
	// via extractFlugoReplace, but the rewrite must never corrupt it).
	in := `module example/backend

go 1.26.1

require github.com/hkdb/flugo v0.1.13

replace github.com/hkdb/flugo => github.com/fork/flugo v9.9.9
`
	out, _ := bumpGoModFlugoRequire([]byte(in), "v0.2.0")
	s := string(out)
	if !strings.Contains(s, "replace github.com/hkdb/flugo => github.com/fork/flugo v9.9.9") {
		t.Errorf("replace line was corrupted:\n%s", s)
	}
	if !strings.Contains(s, "require github.com/hkdb/flugo v0.2.0") {
		t.Errorf("require line not bumped:\n%s", s)
	}
}

func TestBumpGoModFlugoRequire_Idempotent(t *testing.T) {
	in := "require github.com/hkdb/flugo v0.2.0\n"
	if _, changed := bumpGoModFlugoRequire([]byte(in), "v0.2.0"); changed {
		t.Error("expected changed = false when already at target version")
	}
}

func TestBumpPubspecFlugoRefs(t *testing.T) {
	in := `dependencies:
  flutter:
    sdk: flutter
  hardware_key:
    git:
      url: https://github.com/hkdb/flugo.git
      ref: v0.1.9
      path: plugins/hardware_key
  webauthn:
    git:
      ref: v0.1.9
      url: https://github.com/hkdb/flugo.git
      path: plugins/webauthn
  some_other:
    git:
      url: https://github.com/someone/other.git
      ref: v1.0.0
`
	out, changed := bumpPubspecFlugoRefs([]byte(in), "v0.2.0")
	if !changed {
		t.Fatal("expected changed = true")
	}
	s := string(out)
	// Both flugo plugin refs bumped (webauthn has ref BEFORE url — order-independent).
	if strings.Count(s, "ref: v0.2.0") != 2 {
		t.Errorf("expected 2 flugo refs bumped, got:\n%s", s)
	}
	// Non-flugo git dep left untouched.
	if !strings.Contains(s, "ref: v1.0.0") {
		t.Errorf("non-flugo git dep ref was altered:\n%s", s)
	}
	// Paths preserved.
	if !strings.Contains(s, "path: plugins/hardware_key") || !strings.Contains(s, "path: plugins/webauthn") {
		t.Errorf("plugin paths not preserved:\n%s", s)
	}
}

func TestBumpPubspecFlugoRefs_NoFlugoDeps(t *testing.T) {
	in := `dependencies:
  some_other:
    git:
      url: https://github.com/someone/other.git
      ref: v1.0.0
`
	if _, changed := bumpPubspecFlugoRefs([]byte(in), "v0.2.0"); changed {
		t.Error("expected changed = false when no flugo git deps present")
	}
}

// writeFixture lays down a minimal consumer (backend/go.mod + frontend/pubspec.yaml)
// in dir and returns the two paths.
func writeFixture(t *testing.T, dir, goMod, pubspec string) (string, string) {
	t.Helper()
	gm := filepath.Join(dir, "backend", "go.mod")
	ps := filepath.Join(dir, "frontend", "pubspec.yaml")
	for _, p := range []string{gm, ps} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(gm, []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ps, []byte(pubspec), 0o644); err != nil {
		t.Fatal(err)
	}
	return gm, ps
}

const fxGoMod = "module example/backend\n\ngo 1.26.1\n\nrequire github.com/hkdb/flugo v0.2.0\n"

const fxPubspec = `dependencies:
  flutter:
    sdk: flutter
  hardware_key:
    git:
      url: https://github.com/hkdb/flugo.git
      ref: v0.2.0
      path: plugins/hardware_key
`

// End-to-end file-write path (offline: no `go mod tidy`/`flutter pub get`).
func TestSyncFlugoDeps_WritesGoModAndPubspec_WithPlugins(t *testing.T) {
	dir := t.TempDir()
	gm, ps := writeFixture(t, dir, fxGoMod, fxPubspec)

	result := &UpdateResult{}
	if err := syncFlugoDeps(dir, "v0.3.0", true /*plugins*/, true /*offline*/, false /*dryRun*/, result); err != nil {
		t.Fatalf("syncFlugoDeps: %v", err)
	}

	gmOut, _ := os.ReadFile(gm)
	if !strings.Contains(string(gmOut), "github.com/hkdb/flugo v0.3.0") {
		t.Errorf("go.mod require not bumped:\n%s", gmOut)
	}
	psOut, _ := os.ReadFile(ps)
	if !strings.Contains(string(psOut), "ref: v0.3.0") || strings.Contains(string(psOut), "ref: v0.2.0") {
		t.Errorf("pubspec plugin ref not bumped:\n%s", psOut)
	}
	if len(result.Updated) != 2 { // go.mod + pubspec
		t.Errorf("expected 2 updated files, got %v", result.Updated)
	}
}

// Without --plugins, pubspec is left untouched; go.mod still bumps.
func TestSyncFlugoDeps_WithoutPlugins_LeavesPubspec(t *testing.T) {
	dir := t.TempDir()
	gm, ps := writeFixture(t, dir, fxGoMod, fxPubspec)

	result := &UpdateResult{}
	if err := syncFlugoDeps(dir, "v0.3.0", false /*plugins*/, true, false, result); err != nil {
		t.Fatalf("syncFlugoDeps: %v", err)
	}
	if gmOut, _ := os.ReadFile(gm); !strings.Contains(string(gmOut), "flugo v0.3.0") {
		t.Errorf("go.mod not bumped:\n%s", gmOut)
	}
	if psOut, _ := os.ReadFile(ps); !strings.Contains(string(psOut), "ref: v0.2.0") {
		t.Errorf("pubspec should be untouched without --plugins:\n%s", psOut)
	}
}

// A local flugo replace makes the go.mod version bump a no-op (developer-managed).
func TestSyncFlugoDeps_SkipsGoModWhenReplacePresent(t *testing.T) {
	dir := t.TempDir()
	goMod := fxGoMod + "\nreplace github.com/hkdb/flugo => /home/dev/flugo\n"
	gm, _ := writeFixture(t, dir, goMod, fxPubspec)

	result := &UpdateResult{}
	if err := syncFlugoDeps(dir, "v0.3.0", false, true, false, result); err != nil {
		t.Fatalf("syncFlugoDeps: %v", err)
	}
	gmOut, _ := os.ReadFile(gm)
	if strings.Contains(string(gmOut), "flugo v0.3.0") {
		t.Errorf("go.mod require should NOT be bumped when a replace is present:\n%s", gmOut)
	}
	if !strings.Contains(string(gmOut), "replace github.com/hkdb/flugo => /home/dev/flugo") {
		t.Errorf("local replace should be left intact:\n%s", gmOut)
	}
}

// Dry-run writes nothing.
func TestSyncFlugoDeps_DryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	gm, ps := writeFixture(t, dir, fxGoMod, fxPubspec)

	result := &UpdateResult{}
	if err := syncFlugoDeps(dir, "v0.3.0", true, true, true /*dryRun*/, result); err != nil {
		t.Fatalf("syncFlugoDeps: %v", err)
	}
	if gmOut, _ := os.ReadFile(gm); strings.Contains(string(gmOut), "v0.3.0") {
		t.Errorf("dry-run must not write go.mod:\n%s", gmOut)
	}
	if psOut, _ := os.ReadFile(ps); strings.Contains(string(psOut), "v0.3.0") {
		t.Errorf("dry-run must not write pubspec:\n%s", psOut)
	}
}

func TestSyncFlugoDeps_EmptyVersionIsNoop(t *testing.T) {
	// An empty/invalid tag (missing VERSION) must not touch anything.
	for _, tag := range []string{"", "v"} {
		result := &UpdateResult{}
		if err := syncFlugoDeps(t.TempDir(), tag, true, true, false, result); err != nil {
			t.Fatalf("syncFlugoDeps(%q): %v", tag, err)
		}
		if len(result.Updated)+len(result.Created)+len(result.Backed) != 0 {
			t.Errorf("tag %q: expected no file changes, got %+v", tag, result)
		}
	}
}
