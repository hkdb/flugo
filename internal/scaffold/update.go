package scaffold

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/hkdb/flugo/internal/config"
	"github.com/hkdb/flugo/internal/gotoolchain"
)

// UpdateResult summarizes what Update changed.
type UpdateResult struct {
	Updated []string
	Skipped []string
	Created []string
	Backed  []string
}

// frameworkFile maps a template name to its output path relative to the project root.
type frameworkFile struct {
	tmpl   string
	output string
}

// frameworkFiles returns the list of framework-owned files that are always updated.
func frameworkFiles() []frameworkFile {
	return []frameworkFile{
		{"main_dart.tmpl", "frontend/lib/main.dart"},
		{"titlebar_dart.tmpl", "frontend/lib/app/titlebar.dart"},
		{"build_dart.tmpl", "frontend/hook/build.dart"},
		{"ffigen_yaml.tmpl", "frontend/ffigen.yaml"},
		{"makefile.tmpl", "Makefile"},
		// NB: .gitignore is intentionally NOT here. It's a hybrid file — flugo
		// owns only a marked block of generated-file ignores; developer lines are
		// preserved. It's merged separately in Update() (see mergeGitignore), not
		// blindly overwritten.
	}
}

// userFiles returns the list of user-owned files only updated with --all.
func userFiles(appID string) []frameworkFile {
	return []frameworkFile{
		{"app_dart.tmpl", "frontend/lib/app/app.dart"},
		{"pubspec_yaml.tmpl", "frontend/pubspec.yaml"},
		{"main_go.tmpl", "backend/main.go"},
		{"service_go.tmpl", "backend/service.go"},
		{"go_mod.tmpl", "backend/go.mod"},
		{"flugo_yaml.tmpl", "flugo.yaml"},
		{"desktop_file.tmpl", fmt.Sprintf("assets/linux/%s.desktop", appID)},
		{"metainfo_xml.tmpl", fmt.Sprintf("assets/linux/%s.metainfo.xml", appID)},
		{"flatpak_manifest.tmpl", fmt.Sprintf("assets/linux/%s.yaml", appID)},
	}
}

// The flugo-managed block markers inside .gitignore. flugo owns only the lines
// between them (the generated-file ignores); everything else is developer-owned
// and preserved across `flugo update`.
const (
	gitignoreBlockStart = "# ==== flugo-managed (auto-updated by `flugo update`; do not edit inside) ===="
	gitignoreBlockEnd   = "# ==== end flugo-managed ===="
)

// extractManagedBlock returns the flugo-managed block (start marker through end
// marker, inclusive; no trailing newline) from rendered gitignore.tmpl content.
func extractManagedBlock(rendered []byte) (string, error) {
	s := string(rendered)
	i := strings.Index(s, gitignoreBlockStart)
	j := strings.Index(s, gitignoreBlockEnd)
	if i < 0 || j < i {
		return "", fmt.Errorf("gitignore.tmpl is missing the flugo-managed markers")
	}
	return s[i : j+len(gitignoreBlockEnd)], nil
}

// mergeGitignore returns the existing .gitignore content with the flugo-managed
// block refreshed to `block`: replaced in place when the markers are present,
// otherwise appended after one blank line. Every non-block line is preserved
// verbatim, so developer customizations survive.
func mergeGitignore(existing []byte, block string) []byte {
	s := string(existing)
	if i := strings.Index(s, gitignoreBlockStart); i >= 0 {
		if j := strings.Index(s[i:], gitignoreBlockEnd); j >= 0 {
			end := i + j + len(gitignoreBlockEnd)
			return []byte(s[:i] + block + s[end:])
		}
	}
	switch {
	case s == "":
		s = block + "\n"
	case strings.HasSuffix(s, "\n\n"):
		s += block + "\n"
	case strings.HasSuffix(s, "\n"):
		s += "\n" + block + "\n"
	default:
		s += "\n\n" + block + "\n"
	}
	return []byte(s)
}

// extractFlugoReplace reads backend/go.mod and returns the local path from
// any "replace github.com/hkdb/flugo => <path>" directive, or "" if none.
func extractFlugoReplace(projectDir string) string {
	data, err := os.ReadFile(filepath.Join(projectDir, "backend", "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "replace github.com/hkdb/flugo") {
			continue
		}
		parts := strings.Split(line, "=>")
		if len(parts) != 2 {
			continue
		}
		return strings.TrimSpace(parts[1])
	}
	return ""
}

// DataFromConfig derives template data from an existing project's config.
func DataFromConfig(cfg *config.Config, projectDir string, cliVersion string) templateData {
	name := filepath.Base(projectDir)
	return templateData{
		Name:            name,
		DartName:        strings.ReplaceAll(name, "-", "_"),
		AppName:         cfg.App.Name,
		AppID:           cfg.App.ID,
		Module:          cfg.Backend.Module,
		FlugoPath:       extractFlugoReplace(projectDir),
		Description:     cfg.App.Description,
		Version:         cfg.App.Version,
		License:         cfg.App.License,
		URL:             cfg.App.URL,
		Date:            time.Now().Format("2006-01-02"),
		WindowWidth:     cfg.App.Window.Width,
		WindowHeight:    cfg.App.Window.Height,
		MinWindowWidth:  cfg.App.Window.MinWidth,
		MinWindowHeight: cfg.App.Window.MinHeight,
		TitlebarStyle:   cfg.App.Window.TitlebarStyle,
		Runtime:         cfg.Platforms.Linux.Flatpak.Runtime,
		RuntimeVersion:  cfg.Platforms.Linux.Flatpak.RuntimeVersion,
		SDK:             cfg.Platforms.Linux.Flatpak.SDK,
		Permissions:     cfg.Platforms.Linux.Flatpak.Permissions,
		FlugoVersion:    cliVersion,
		FlugoVersionTag: "v" + strings.TrimPrefix(cliVersion, "v"),
		URLScheme:       cfg.App.URLScheme,
		GoArches:        gotoolchain.Arches,
	}
}

// configDependentFiles returns framework files that depend on flugo.yaml config values
// (e.g. titlebar_style). These are re-synced on every run/build.
func configDependentFiles() []frameworkFile {
	return []frameworkFile{
		{"main_dart.tmpl", "frontend/lib/main.dart"},
		{"titlebar_dart.tmpl", "frontend/lib/app/titlebar.dart"},
		// The native-assets build hook is framework-owned and must track the CLI
		// version (its cgo/pkg-config logic changes between flugo releases), so
		// re-render it on every build/run like the files above — otherwise a
		// `flugo build` after a CLI bump silently keeps running the stale hook
		// until someone remembers to `flugo update`.
		{"build_dart.tmpl", "frontend/hook/build.dart"},
	}
}

// SyncConfigFiles re-renders config-dependent framework files, silently overwriting
// only if content changed. Called by run/build so config changes take effect without
// requiring `flugo update`.
func SyncConfigFiles(projectDir string, cfg *config.Config, cliVersion string) error {
	data := DataFromConfig(cfg, projectDir, cliVersion)

	tmpl, err := parseTemplates()
	if err != nil {
		return err
	}

	depFiles := configDependentFiles()
	if cfg.App.Window.TitlebarStyle == "custom" {
		filtered := depFiles[:0]
		for _, f := range depFiles {
			if f.tmpl != "titlebar_dart.tmpl" {
				filtered = append(filtered, f)
			}
		}
		depFiles = filtered
	}

	for _, f := range depFiles {
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, f.tmpl, data); err != nil {
			return fmt.Errorf("executing template %s: %w", f.tmpl, err)
		}
		rendered := buf.Bytes()

		fullPath := filepath.Join(projectDir, f.output)

		existing, err := os.ReadFile(fullPath)
		if err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("reading %s: %w", f.output, err)
			}
			if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
				return fmt.Errorf("creating directory for %s: %w", f.output, err)
			}
			if err := os.WriteFile(fullPath, rendered, 0o644); err != nil {
				return fmt.Errorf("writing %s: %w", f.output, err)
			}
			continue
		}

		if bytes.Equal(existing, rendered) {
			continue
		}

		if err := os.WriteFile(fullPath, rendered, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", f.output, err)
		}
	}

	return nil
}

// Update re-renders framework-owned template files into an existing project.
// If all is true, user-owned files are also updated.
// If dryRun is true, no files are written.
func Update(projectDir string, cfg *config.Config, all bool, dryRun bool, cliVersion, tag string, plugins, offline bool) (*UpdateResult, error) {
	if err := cfg.ValidateForGeneration(); err != nil {
		return nil, err
	}
	data := DataFromConfig(cfg, projectDir, cliVersion)

	tmpl, err := parseTemplates()
	if err != nil {
		return nil, err
	}

	// Idempotent platform-file fixups existing apps pick up on update
	// (scaffold applies the same at create time).
	if !dryRun {
		if err := patchAndroidManifestPermissions(filepath.Join(projectDir, "frontend")); err != nil {
			return nil, err
		}
		// Stamp app.id onto the native bundle identifiers (fixes projects
		// scaffolded before this — their IDs are still com.example.<name>).
		// Runs before renderMainActivity below so the relocated Kotlin package
		// is what gets re-rendered.
		if err := applyBundleID(filepath.Join(projectDir, "frontend"), cfg.App.ID); err != nil {
			return nil, fmt.Errorf("applying bundle id: %w", err)
		}
		// Stamp app.name onto the macOS AppInfo.xcconfig PRODUCT_NAME so the built
		// .app is named after the app, not the flutter project (ic_app).
		if err := patchMacOSAppInfo(filepath.Join(projectDir, "frontend"), cfg.App.Name, cfg.App.ID); err != nil {
			return nil, fmt.Errorf("applying macos app info: %w", err)
		}
	}

	files := frameworkFiles()
	if cfg.App.Window.TitlebarStyle == "custom" {
		filtered := files[:0]
		for _, f := range files {
			if f.tmpl != "titlebar_dart.tmpl" {
				filtered = append(filtered, f)
			}
		}
		files = filtered
	}
	if all {
		files = append(files, userFiles(data.AppID)...)
	}

	result := &UpdateResult{}

	for _, f := range files {
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, f.tmpl, data); err != nil {
			return nil, fmt.Errorf("executing template %s: %w", f.tmpl, err)
		}
		if err := applyRendered(result, f.output, filepath.Join(projectDir, f.output), buf.Bytes(), dryRun); err != nil {
			return nil, err
		}
	}

	// MainActivity is a flugo-managed platform file (like main.dart/titlebar.dart):
	// refresh it from the template so it never drifts — flugo owns it end to end
	// (create writes it, update keeps it current). Skipped when there's no Android
	// target. This is what lets apps like ic-app stop hand-maintaining a copy.
	if maPath, rendered, merrr := renderMainActivity(filepath.Join(projectDir, "frontend")); merrr == nil {
		rel, relErr := filepath.Rel(projectDir, maPath)
		if relErr != nil {
			rel = maPath
		}
		if err := applyRendered(result, rel, maPath, rendered, dryRun); err != nil {
			return nil, err
		}
	} else if !errors.Is(merrr, errNoAndroidMainActivity) {
		return nil, merrr
	}

	// .gitignore is a hybrid file: flugo owns only a marked block of generated-
	// file ignores; developer lines outside it must survive. Render the template
	// for the authoritative current block, then splice it into the existing file
	// (replace the marked region, or append when absent) rather than overwriting.
	{
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, "gitignore.tmpl", data); err != nil {
			return nil, fmt.Errorf("executing template gitignore.tmpl: %w", err)
		}
		block, err := extractManagedBlock(buf.Bytes())
		if err != nil {
			return nil, err
		}
		giPath := filepath.Join(projectDir, ".gitignore")
		existing, rerr := os.ReadFile(giPath)
		switch {
		case rerr == nil:
			if err := applyRendered(result, ".gitignore", giPath, mergeGitignore(existing, block), dryRun); err != nil {
				return nil, err
			}
		case os.IsNotExist(rerr):
			// No .gitignore at all (unusual on update) — write the full template.
			if err := applyRendered(result, ".gitignore", giPath, buf.Bytes(), dryRun); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("reading .gitignore: %w", rerr)
		}
	}

	// Sync flugo dependency version pins (backend/go.mod always; plugin git refs
	// in pubspec only with --plugins) to this CLI's tag, so they never drift.
	if err := syncFlugoDeps(projectDir, tag, plugins, offline, dryRun, result); err != nil {
		return nil, err
	}

	// Stamp flugo_version after a successful non-dry-run update.
	if !dryRun && cliVersion != "" {
		cfg.FlugoVersion = cliVersion
		if err := config.Save(projectDir, cfg); err != nil {
			return nil, fmt.Errorf("saving flugo_version: %w", err)
		}
	}

	return result, nil
}

// syncFlugoDeps pins a consuming app's flugo dependency references to `tag`
// (e.g. "v0.2.1", derived from the flugo CLI's embedded VERSION): backend/go.mod's
// flugo require is always bumped, and — only when plugins is true — every
// flugo-repo plugin git `ref:` in frontend/pubspec.yaml. File edits reuse
// applyRendered (backup + result records, honors dryRun). When !offline && !dryRun
// it runs `go mod tidy` / `flutter pub get` to reconcile lockfiles; those failures
// are warnings, not fatal. A local flugo `replace` in backend/go.mod is detected
// and left intact (its version bump is skipped) — local overrides are
// developer-managed.
func syncFlugoDeps(projectDir, tag string, plugins, offline, dryRun bool, result *UpdateResult) error {
	if tag == "" || tag == "v" {
		fmt.Println("  ⚠️  could not determine flugo version (empty VERSION) — skipping dependency sync.")
		return nil
	}

	// backend/go.mod flugo require — always.
	goModPath := filepath.Join(projectDir, "backend", "go.mod")
	if data, err := os.ReadFile(goModPath); err == nil {
		if replaced := extractFlugoReplace(projectDir); replaced != "" {
			fmt.Printf("  ℹ️  backend/go.mod has a local flugo replace (%s) — not bumping its version.\n", replaced)
		} else if newData, changed := bumpGoModFlugoRequire(data, tag); changed {
			if err := applyRendered(result, "backend/go.mod", goModPath, newData, dryRun); err != nil {
				return err
			}
			if !offline && !dryRun {
				fmt.Println("  📦 go mod tidy (backend)...")
				tidy := exec.Command("go", "mod", "tidy")
				tidy.Dir = filepath.Join(projectDir, "backend")
				if out, err := tidy.CombinedOutput(); err != nil {
					fmt.Printf("  ⚠️  go mod tidy failed (run it manually): %v\n%s\n", err, out)
				}
			}
		}
	}

	if !plugins {
		return nil
	}

	// frontend/pubspec.yaml flugo plugin git refs — opt-in via --plugins.
	pubspecPath := filepath.Join(projectDir, "frontend", "pubspec.yaml")
	if data, err := os.ReadFile(pubspecPath); err == nil {
		if newData, changed := bumpPubspecFlugoRefs(data, tag); changed {
			if err := applyRendered(result, "frontend/pubspec.yaml", pubspecPath, newData, dryRun); err != nil {
				return err
			}
			if !offline && !dryRun {
				fmt.Println("  📦 flutter pub get (frontend)...")
				pg := exec.Command("flutter", "pub", "get")
				pg.Dir = filepath.Join(projectDir, "frontend")
				if out, err := pg.CombinedOutput(); err != nil {
					fmt.Printf("  ⚠️  flutter pub get failed (run it manually): %v\n%s\n", err, out)
				}
			}
		}
	}

	return nil
}

// flugoRequireRe matches the flugo require line in go.mod — both the block form
// (`\tgithub.com/hkdb/flugo v1.2.3`) and the single-line form
// (`require github.com/hkdb/flugo v1.2.3`) — capturing everything up to the
// version token so only the version is replaced (a trailing `// indirect` stays
// intact). It deliberately does NOT match a `replace github.com/hkdb/flugo`
// line (that starts with "replace "; handled via extractFlugoReplace).
var flugoRequireRe = regexp.MustCompile(`(?m)^(\s*(?:require\s+)?github\.com/hkdb/flugo\s+)v\S+`)

// bumpGoModFlugoRequire rewrites the flugo require version to tag, returning the
// new bytes and whether anything changed.
func bumpGoModFlugoRequire(data []byte, tag string) ([]byte, bool) {
	out := flugoRequireRe.ReplaceAll(data, []byte("${1}"+tag))
	return out, !bytes.Equal(out, data)
}

// bumpPubspecFlugoRefs rewrites the `ref:` of every pubspec git dependency whose
// `url:` points at the flugo repo, to tag. It scans `git:` mapping blocks by
// indentation so it is order-independent and leaves all other deps untouched.
func bumpPubspecFlugoRefs(data []byte, tag string) ([]byte, bool) {
	lines := strings.Split(string(data), "\n")
	changed := false
	i := 0
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) != "git:" {
			i++
			continue
		}
		gitIndent := leadingSpaces(lines[i])
		// Collect the block body (deeper-indented lines) and detect a flugo url.
		j := i + 1
		isFlugo := false
		for j < len(lines) {
			if strings.TrimSpace(lines[j]) == "" {
				j++
				continue
			}
			if leadingSpaces(lines[j]) <= gitIndent {
				break
			}
			t := strings.TrimSpace(lines[j])
			if strings.HasPrefix(t, "url:") && strings.Contains(t, "github.com/hkdb/flugo") {
				isFlugo = true
			}
			j++
		}
		if isFlugo {
			for k := i + 1; k < j; k++ {
				if !strings.HasPrefix(strings.TrimSpace(lines[k]), "ref:") {
					continue
				}
				indent := lines[k][:leadingSpaces(lines[k])]
				newLine := indent + "ref: " + tag
				if lines[k] != newLine {
					lines[k] = newLine
					changed = true
				}
			}
		}
		i = j
	}
	return []byte(strings.Join(lines, "\n")), changed
}

// leadingSpaces returns the number of leading space/tab characters in s.
func leadingSpaces(s string) int {
	n := 0
	for n < len(s) && (s[n] == ' ' || s[n] == '\t') {
		n++
	}
	return n
}

// applyRendered writes one managed file's rendered content into the project and
// records the outcome in result: missing→create, byte-equal→skip, changed→back
// up as .bak then overwrite. Honors dryRun. relPath is the label used in the
// report; fullPath is where the bytes are written.
func applyRendered(result *UpdateResult, relPath, fullPath string, rendered []byte, dryRun bool) error {
	existing, err := os.ReadFile(fullPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("reading %s: %w", relPath, err)
		}
		if !dryRun {
			if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
				return fmt.Errorf("creating directory for %s: %w", relPath, err)
			}
			if err := os.WriteFile(fullPath, rendered, 0o644); err != nil {
				return fmt.Errorf("writing %s: %w", relPath, err)
			}
		}
		result.Created = append(result.Created, relPath)
		return nil
	}

	if bytes.Equal(existing, rendered) {
		result.Skipped = append(result.Skipped, relPath)
		return nil
	}

	if !dryRun {
		if err := os.WriteFile(fullPath+".bak", existing, 0o644); err != nil {
			return fmt.Errorf("backing up %s: %w", relPath, err)
		}
		result.Backed = append(result.Backed, relPath+".bak")
		if err := os.WriteFile(fullPath, rendered, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", relPath, err)
		}
	}
	result.Updated = append(result.Updated, relPath)
	return nil
}
