package packager

import (
	"fmt"
	"os"
	"path/filepath"
)

// packageWindows packages the Flutter Windows build.
func (p *Packager) packageWindows() error {
	fmt.Println("Packaging for Windows...")

	buildDir := filepath.Join(p.projectDir, "frontend", "build", "windows", "x64", "runner", "Release")
	if _, err := os.Stat(buildDir); err != nil {
		return fmt.Errorf("build not found at %s; run 'flugo build windows' first", buildDir)
	}

	outputDir := filepath.Join(p.projectDir, "build", "package", "windows")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}

	fmt.Printf("Windows build is at: %s\n", buildDir)
	fmt.Printf("Copy contents to %s for distribution or create an MSIX/installer.\n", outputDir)
	return nil
}
