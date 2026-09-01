package packager

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// packageLinux creates a tarball with the binary, shared libs, desktop file, and icons.
func (p *Packager) packageLinux() error {
	fmt.Println("Packaging for Linux...")

	buildDir := filepath.Join(p.projectDir, "frontend", "build", "linux", "x64", "release", "bundle")
	if _, err := os.Stat(buildDir); err != nil {
		return fmt.Errorf("build not found at %s; run 'flugo build linux' first", buildDir)
	}

	outputDir := filepath.Join(p.projectDir, "build", "package", "linux")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}

	appName := p.cfg.App.Name
	version := p.cfg.App.Version
	tarName := fmt.Sprintf("%s-%s-linux-x86_64.tar.gz", appName, version)
	tarPath := filepath.Join(outputDir, tarName)

	// Create tarball from the Flutter build bundle.
	cmd := exec.Command("tar", "-czf", tarPath, "-C", filepath.Dir(buildDir), "bundle")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("creating tarball: %w", err)
	}

	fmt.Printf("Package created: %s\n", tarPath)
	return nil
}
