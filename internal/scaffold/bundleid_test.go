package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAndroidPackageID(t *testing.T) {
	cases := map[string]string{
		"io.github.instacryptio.ic-app": "io.github.instacryptio.ic_app",
		"com.example.app":               "com.example.app",
		"com.foo.9bar":                  "com.foo._9bar",
		"a.b-c.d--e":                    "a.b_c.d__e",
	}
	for in, want := range cases {
		if got := androidPackageID(in); got != want {
			t.Errorf("androidPackageID(%q) = %q, want %q", in, got, want)
		}
	}
}

// writeFrontend stands up a minimal flutter-created native tree with the default
// com.example.foo bundle IDs.
func writeFrontend(t *testing.T) string {
	t.Helper()
	fe := t.TempDir()
	must := func(p, content string) {
		full := filepath.Join(fe, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("android/app/build.gradle.kts", "android {\n    namespace = \"com.example.foo\"\n    defaultConfig {\n        applicationId = \"com.example.foo\"\n    }\n}\n")
	must("android/app/src/main/kotlin/com/example/foo/MainActivity.kt", "package com.example.foo\n\nclass MainActivity\n")
	pbx := "\t\t\t\tPRODUCT_BUNDLE_IDENTIFIER = com.example.foo;\n\t\t\t\tPRODUCT_BUNDLE_IDENTIFIER = com.example.foo.RunnerTests;\n"
	must("ios/Runner.xcodeproj/project.pbxproj", pbx)
	must("macos/Runner.xcodeproj/project.pbxproj", pbx)
	return fe
}

func TestApplyBundleID(t *testing.T) {
	fe := writeFrontend(t)
	if err := applyBundleID(fe, "io.github.instacryptio.ic-app"); err != nil {
		t.Fatalf("applyBundleID: %v", err)
	}

	// Android gradle uses the sanitized package.
	gradle, _ := os.ReadFile(filepath.Join(fe, "android/app/build.gradle.kts"))
	for _, want := range []string{`applicationId = "io.github.instacryptio.ic_app"`, `namespace = "io.github.instacryptio.ic_app"`} {
		if !strings.Contains(string(gradle), want) {
			t.Errorf("gradle missing %q; got:\n%s", want, gradle)
		}
	}

	// MainActivity relocated to the new package dir, old dir pruned.
	newMA := filepath.Join(fe, "android/app/src/main/kotlin/io/github/instacryptio/ic_app/MainActivity.kt")
	ma, err := os.ReadFile(newMA)
	if err != nil {
		t.Fatalf("MainActivity not at new path: %v", err)
	}
	if !strings.Contains(string(ma), "package io.github.instacryptio.ic_app") {
		t.Errorf("MainActivity package not rewritten; got:\n%s", ma)
	}
	if _, err := os.Stat(filepath.Join(fe, "android/app/src/main/kotlin/com/example/foo/MainActivity.kt")); !os.IsNotExist(err) {
		t.Error("old MainActivity.kt still present")
	}
	if _, err := os.Stat(filepath.Join(fe, "android/app/src/main/kotlin/com/example")); !os.IsNotExist(err) {
		t.Error("old empty package dir not pruned")
	}

	// iOS/macOS pbxproj: Runner gets the (quoted, hyphenated) id; RunnerTests the suffix.
	for _, plat := range []string{"ios", "macos"} {
		pbx, _ := os.ReadFile(filepath.Join(fe, plat, "Runner.xcodeproj", "project.pbxproj"))
		if !strings.Contains(string(pbx), `PRODUCT_BUNDLE_IDENTIFIER = "io.github.instacryptio.ic-app";`) {
			t.Errorf("%s Runner bundle id wrong; got:\n%s", plat, pbx)
		}
		if !strings.Contains(string(pbx), `PRODUCT_BUNDLE_IDENTIFIER = "io.github.instacryptio.ic-app.RunnerTests";`) {
			t.Errorf("%s RunnerTests bundle id wrong; got:\n%s", plat, pbx)
		}
	}
}

func TestApplyBundleIDIdempotent(t *testing.T) {
	fe := writeFrontend(t)
	if err := applyBundleID(fe, "io.github.instacryptio.ic-app"); err != nil {
		t.Fatalf("first: %v", err)
	}
	// A second run must be a clean no-op (not double-suffix RunnerTests, not move again).
	if err := applyBundleID(fe, "io.github.instacryptio.ic-app"); err != nil {
		t.Fatalf("second: %v", err)
	}
	pbx, _ := os.ReadFile(filepath.Join(fe, "ios/Runner.xcodeproj/project.pbxproj"))
	if strings.Contains(string(pbx), "RunnerTests.RunnerTests") {
		t.Errorf("RunnerTests suffix doubled on re-run:\n%s", pbx)
	}
	if _, err := os.Stat(filepath.Join(fe, "android/app/src/main/kotlin/io/github/instacryptio/ic_app/MainActivity.kt")); err != nil {
		t.Errorf("MainActivity missing after idempotent re-run: %v", err)
	}
}

func TestApplyBundleIDNoPlatforms(t *testing.T) {
	// Bare dir (no android/ios/macos) → no error.
	if err := applyBundleID(t.TempDir(), "com.example.app"); err != nil {
		t.Errorf("expected no-op on a platformless dir, got %v", err)
	}
}
