package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// The app's version has ONE source: `app: version:` in flugo.yaml.
// `flugo gen` copies it into frontend/pubspec.yaml (which Flutter turns into
// version.json / versionName / CFBundleShortVersionString); `flugo build`
// stamps it into the Go backend and refuses to build while the pubspec copy
// is stale.

// pubspecVersionRe matches the top-level `version:` line: base version, then
// an optional Flutter build number (`1.2.3+4`), which is preserved.
var pubspecVersionRe = regexp.MustCompile(`(?m)^(version:[ \t]*)([^\s+#]+)(\+[^\s#]*)?([ \t]*(?:#.*)?)$`)

func pubspecPath(projectDir string) string {
	return filepath.Join(projectDir, "frontend", "pubspec.yaml")
}

// PubspecVersion returns the base version recorded in frontend/pubspec.yaml
// ("1.2.3" of "1.2.3+4"), or "" when the file or the line is absent.
func PubspecVersion(projectDir string) (string, error) {
	data, err := os.ReadFile(pubspecPath(projectDir))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	m := pubspecVersionRe.FindSubmatch(data)
	if m == nil {
		return "", nil
	}
	return string(m[2]), nil
}

// SyncAppVersion rewrites pubspec's `version:` line to version, keeping any
// `+build` suffix and touching nothing else in the developer-owned file. It
// reports whether the file changed; with dryRun it reports without writing.
// An empty version is a no-op.
func SyncAppVersion(projectDir, version string, dryRun bool) (bool, error) {
	if version == "" {
		return false, nil
	}
	path := pubspecPath(projectDir)
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	m := pubspecVersionRe.FindSubmatchIndex(data)
	if m == nil {
		return false, fmt.Errorf("%s has no top-level version: line", path)
	}
	// Replace only the base version (group 2); prefix, +build and trailing
	// comment stay as they are.
	updated := append([]byte{}, data[:m[4]]...)
	updated = append(updated, version...)
	updated = append(updated, data[m[5]:]...)
	if string(updated) == string(data) {
		return false, nil
	}
	if dryRun {
		return true, nil
	}
	return true, os.WriteFile(path, updated, 0o644)
}

// CheckAppVersion fails when the pubspec copy of the version is stale, so a
// build cannot ship a number that differs from flugo.yaml. Builds never edit
// source files; the fix is `flugo gen`.
func CheckAppVersion(projectDir, version string) error {
	if version == "" {
		return nil
	}
	have, err := PubspecVersion(projectDir)
	if err != nil {
		return err
	}
	if have == "" || have == version {
		return nil
	}
	return fmt.Errorf("frontend/pubspec.yaml version is %s but flugo.yaml app version is %s — run 'flugo gen'", have, version)
}
