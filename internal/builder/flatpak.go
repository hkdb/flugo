package builder

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// buildFlatpak builds a Flatpak package using flatpak-builder.
func (b *Builder) buildFlatpak() error {
	fmt.Println("\n📦 Building Flatpak...")

	if _, err := exec.LookPath("flatpak-builder"); err != nil {
		return fmt.Errorf("flatpak-builder not found in PATH — install it to build Flatpak packages")
	}

	manifestPath := filepath.Join(b.projectDir, "assets", "linux", b.cfg.App.ID+".yaml")
	if _, err := stat(manifestPath); err != nil {
		return fmt.Errorf("flatpak manifest not found at %s — run 'flugo flathub' first", manifestPath)
	}

	buildDir := filepath.Join(b.buildDir(), "flatpak")

	args := []string{
		"--user", "--install", "--force-clean",
		buildDir,
		manifestPath,
	}

	fmt.Println("  ⚙️  Running flatpak-builder...")
	if err := runCommand("flatpak-builder", args, b.projectDir, nil); err != nil {
		return fmt.Errorf("flatpak-builder failed: %w", err)
	}

	fmt.Println("  ✅ Flatpak build complete!")
	return nil
}
