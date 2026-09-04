package scaffold

// Bundle-identifier application. `flutter create` defaults the Android/iOS/macOS
// bundle IDs to com.example.<name>; flugo must stamp the project's real app.id
// onto them (they drive store identity, deep-link/webcredential association, and
// signing). This runs at scaffold (create) AND on `flugo update`, idempotently,
// so existing projects are corrected too. app.id may contain hyphens (valid in
// Apple bundle IDs); Android package segments cannot, so those are sanitized.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	androidApplicationIDRe = regexp.MustCompile(`applicationId\s*=\s*"[^"]*"`)
	androidNamespaceRe     = regexp.MustCompile(`namespace\s*=\s*"[^"]*"`)
	kotlinPackageRe        = regexp.MustCompile(`(?m)^package .*$`)
	xcodeBundleIDRe        = regexp.MustCompile(`PRODUCT_BUNDLE_IDENTIFIER = [^;]+;`)
)

// applyBundleID stamps appID onto the Android/iOS/macOS native projects (Android
// sanitized to a valid package). Idempotent; each platform is a no-op when not
// generated. Call BEFORE renderMainActivity so the relocated package is picked up.
func applyBundleID(frontendDir, appID string) error {
	if err := patchAndroidBundleID(frontendDir, androidPackageID(appID)); err != nil {
		return fmt.Errorf("android bundle id: %w", err)
	}
	if err := patchXcodeBundleID(filepath.Join(frontendDir, "ios", "Runner.xcodeproj", "project.pbxproj"), appID); err != nil {
		return fmt.Errorf("ios bundle id: %w", err)
	}
	if err := patchXcodeBundleID(filepath.Join(frontendDir, "macos", "Runner.xcodeproj", "project.pbxproj"), appID); err != nil {
		return fmt.Errorf("macos bundle id: %w", err)
	}
	return nil
}

// androidPackageID converts an app.id into a valid Android applicationId /
// namespace: each dotted segment must be a Java identifier, so hyphens (and any
// other invalid char) become underscores, and a digit-leading segment is
// underscore-prefixed. e.g. io.github.instacryptio.ic-app → ...ic_app.
func androidPackageID(appID string) string {
	segs := strings.Split(appID, ".")
	for i, s := range segs {
		segs[i] = sanitizeAndroidSegment(s)
	}
	return strings.Join(segs, ".")
}

func sanitizeAndroidSegment(seg string) string {
	var b strings.Builder
	for i, r := range seg {
		switch {
		case r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			if i == 0 {
				b.WriteRune('_')
			}
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}

// patchAndroidBundleID rewrites applicationId + namespace in the app-level
// build.gradle.kts and relocates MainActivity.kt into the new package.
func patchAndroidBundleID(frontendDir, pkg string) error {
	gradle := filepath.Join(frontendDir, "android", "app", "build.gradle.kts")
	raw, err := os.ReadFile(gradle)
	if os.IsNotExist(err) {
		return nil // android platform not generated
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", gradle, err)
	}
	content := androidApplicationIDRe.ReplaceAllString(string(raw), fmt.Sprintf("applicationId = %q", pkg))
	content = androidNamespaceRe.ReplaceAllString(content, fmt.Sprintf("namespace = %q", pkg))
	if err := os.WriteFile(gradle, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", gradle, err)
	}
	return relocateMainActivity(frontendDir, pkg)
}

// relocateMainActivity moves MainActivity.kt to the kotlin/<pkg-path>/ directory
// and rewrites its `package` line, so it matches the new namespace (the manifest
// references it as ".MainActivity", relative to the namespace). Empty leftover
// package dirs are pruned. No-op when already in place.
func relocateMainActivity(frontendDir, pkg string) error {
	kotlinDir := filepath.Join(frontendDir, "android", "app", "src", "main", "kotlin")
	if _, err := os.Stat(kotlinDir); err != nil {
		return nil
	}
	var cur string
	err := filepath.Walk(kotlinDir, func(p string, info os.FileInfo, err error) error {
		if err == nil && info != nil && info.Name() == "MainActivity.kt" {
			cur = p
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking %s: %w", kotlinDir, err)
	}
	if cur == "" {
		return nil
	}
	raw, err := os.ReadFile(cur)
	if err != nil {
		return fmt.Errorf("reading MainActivity.kt: %w", err)
	}
	content := kotlinPackageRe.ReplaceAllString(string(raw), "package "+pkg)

	newDir := filepath.Join(append([]string{kotlinDir}, strings.Split(pkg, ".")...)...)
	newPath := filepath.Join(newDir, "MainActivity.kt")
	if newPath == cur {
		return os.WriteFile(cur, []byte(content), 0o644)
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", newDir, err)
	}
	if err := os.WriteFile(newPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", newPath, err)
	}
	if err := os.Remove(cur); err != nil {
		return fmt.Errorf("removing old MainActivity.kt: %w", err)
	}
	pruneEmptyDirs(filepath.Dir(cur), kotlinDir)
	return nil
}

// pruneEmptyDirs removes now-empty directories from dir upward, stopping at
// (and never removing) stop.
func pruneEmptyDirs(dir, stop string) {
	for dir != stop && strings.HasPrefix(dir, stop+string(os.PathSeparator)) {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// patchXcodeBundleID rewrites every PRODUCT_BUNDLE_IDENTIFIER in a pbxproj to
// bundleID (RunnerTests targets get the ".RunnerTests" suffix). The value is
// always quoted so hyphens (valid in Apple bundle IDs) don't corrupt the
// project file. Idempotent.
func patchXcodeBundleID(pbxproj, bundleID string) error {
	raw, err := os.ReadFile(pbxproj)
	if os.IsNotExist(err) {
		return nil // platform not generated
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", pbxproj, err)
	}
	content := xcodeBundleIDRe.ReplaceAllStringFunc(string(raw), func(m string) string {
		id := bundleID
		if strings.Contains(m, ".RunnerTests") {
			id = bundleID + ".RunnerTests"
		}
		return fmt.Sprintf("PRODUCT_BUNDLE_IDENTIFIER = %q;", id)
	})
	return os.WriteFile(pbxproj, []byte(content), 0o644)
}
