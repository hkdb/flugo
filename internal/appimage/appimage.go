// Package appimage generates AppImage packaging assets.
package appimage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/hkdb/flugo/internal/config"
)

type templateData struct {
	Name    string
	BinName string // Dart-safe name (hyphens → underscores)
}

// Generate creates AppImage packaging assets in assets/linux/appimage/.
func Generate(projectDir string, cfg *config.Config) error {
	fmt.Println("\n📦 Generating AppImage assets...")

	outputDir := filepath.Join(projectDir, "assets", "linux", "appimage")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}

	// Derive short name from app ID (last component).
	name := cfg.App.ID
	if parts := strings.Split(name, "."); len(parts) > 0 {
		name = parts[len(parts)-1]
	}

	data := templateData{
		Name:    name,
		BinName: strings.ReplaceAll(name, "-", "_"),
	}

	// Generate AppRun.
	appRunPath := filepath.Join(outputDir, "AppRun")
	tmpl, err := template.New("apprun").Parse(appRunTmpl)
	if err != nil {
		return fmt.Errorf("parsing AppRun template: %w", err)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("executing AppRun template: %w", err)
	}

	if err := os.WriteFile(appRunPath, []byte(buf.String()), 0o755); err != nil {
		return fmt.Errorf("writing AppRun: %w", err)
	}

	// Copy .desktop file.
	desktopSrc := filepath.Join(projectDir, "assets", "linux", cfg.App.ID+".desktop")
	desktopDst := filepath.Join(outputDir, cfg.App.ID+".desktop")
	if err := copyFile(desktopSrc, desktopDst); err != nil {
		return fmt.Errorf("copying .desktop file: %w", err)
	}

	// Copy icon (try 256, then other sizes, then SVG).
	if err := copyIcon(projectDir, outputDir, name, cfg); err != nil {
		return fmt.Errorf("copying icon: %w", err)
	}

	fmt.Println("\n  Generated:")
	fmt.Println("    assets/linux/appimage/AppRun")
	fmt.Printf("    assets/linux/appimage/%s.desktop\n", cfg.App.ID)
	fmt.Printf("    assets/linux/appimage/%s.png\n", name)

	return nil
}

func copyIcon(projectDir, outputDir, name string, cfg *config.Config) error {
	iconsDir := filepath.Join(projectDir, "assets", "icons")

	// Try PNG sizes in preferred order.
	sizes := []int{256, 512, 128, 64}
	for _, size := range sizes {
		src := filepath.Join(iconsDir, fmt.Sprintf("icon-%dx%d.png", size, size))
		if _, err := os.Stat(src); err == nil {
			return copyFile(src, filepath.Join(outputDir, name+".png"))
		}
	}

	// Fall back to SVG source.
	svgSrc := filepath.Join(projectDir, cfg.Icons.Source)
	if _, err := os.Stat(svgSrc); err == nil {
		// Copy SVG but name it .png — AppImage tooling may handle this,
		// but ideally icons should be generated first with `flugo icons`.
		fmt.Println("  ⚠️  No PNG icons found. Using source SVG — run 'flugo icons' first for best results.")
		return copyFile(svgSrc, filepath.Join(outputDir, name+".png"))
	}

	return fmt.Errorf("no icon found in assets/icons/ — run 'flugo icons' first")
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	// Preserve source file permissions.
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, data, info.Mode())
}

const appRunTmpl = `#!/bin/bash
HERE="$(dirname "$(readlink -f "$0")")"
export LD_LIBRARY_PATH="$HERE/usr/lib:$LD_LIBRARY_PATH"
exec "$HERE/usr/bin/{{.BinName}}" "$@"
`
