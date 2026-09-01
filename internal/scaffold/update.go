package scaffold

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
		{"gitignore.tmpl", ".gitignore"},
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
func Update(projectDir string, cfg *config.Config, all bool, dryRun bool, cliVersion string) (*UpdateResult, error) {
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

	// Stamp flugo_version after a successful non-dry-run update.
	if !dryRun && cliVersion != "" {
		cfg.FlugoVersion = cliVersion
		if err := config.Save(projectDir, cfg); err != nil {
			return nil, fmt.Errorf("saving flugo_version: %w", err)
		}
	}

	return result, nil
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
