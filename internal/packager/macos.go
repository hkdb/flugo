package packager

import (
	"fmt"
	"os"
	"path/filepath"
)

// packageMacOS packages the Flutter macOS build as an .app bundle.
func (p *Packager) packageMacOS() error {
	fmt.Println("Packaging for macOS...")

	buildDir := filepath.Join(p.projectDir, "frontend", "build", "macos", "Build", "Products", "Release")
	if _, err := os.Stat(buildDir); err != nil {
		return fmt.Errorf("build not found at %s; run 'flugo build macos' first", buildDir)
	}

	outputDir := filepath.Join(p.projectDir, "build", "package", "macos")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}

	fmt.Printf("macOS .app bundle is at: %s\n", buildDir)
	fmt.Printf("Copy the .app from %s to %s for distribution.\n", buildDir, outputDir)
	return nil
}
