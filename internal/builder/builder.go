// Package builder orchestrates Go and Flutter builds for all target platforms.
package builder

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hkdb/flugo/internal/config"
)

// Builder coordinates building a Flugo project.
type Builder struct {
	projectDir string
	cfg        *config.Config
}

// New creates a Builder.
func New(projectDir string, cfg *config.Config) *Builder {
	return &Builder{
		projectDir: projectDir,
		cfg:        cfg,
	}
}

// Build builds for the specified platform.
func (b *Builder) Build(platform string, release bool) error {
	fmt.Printf("\n🏗️  Building for %s...\n", platform)

	switch platform {
	case "linux", "macos", "windows":
		if err := b.buildDesktop(platform, release); err != nil {
			return err
		}
	case "android":
		if err := b.buildAndroid(release); err != nil {
			return err
		}
	case "ios":
		if err := b.buildIOS(release); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown platform: %s (valid: linux, macos, windows, android, ios, all)", platform)
	}

	fmt.Printf("  ✅ Build complete for %s\n", platform)

	if err := b.linkBuildOutput(platform); err != nil {
		fmt.Printf("  ⚠️  Could not create output link: %v\n", err)
		return nil
	}

	paths := b.flutterBuildOutputPaths(platform)
	for suffix := range paths {
		if suffix == "" {
			fmt.Printf("  📦 Output: build/bin/%s/\n", platform)
			continue
		}
		fmt.Printf("  📦 Output: build/bin/%s/%s/\n", platform, suffix)
	}

	return nil
}

// BuildAll builds for all platforms, plus AppImage and Flatpak if scaffolded.
func (b *Builder) BuildAll(release bool) error {
	platforms := []string{"linux", "macos", "windows", "android", "ios"}
	var errs []error
	for _, p := range platforms {
		if err := b.Build(p, release); err != nil {
			// Continue to the remaining platforms so one unavailable target
			// (e.g. macos/ios on Linux) doesn't hide the others + AppImage/Flatpak.
			fmt.Printf("  ⚠️  %s build failed: %v\n", p, err)
			errs = append(errs, fmt.Errorf("building %s: %w", p, err))
		}
	}

	// Build AppImage if scaffolded.
	appRunPath := filepath.Join(b.projectDir, "assets", "linux", "appimage", "AppRun")
	if _, err := os.Stat(appRunPath); err == nil {
		if err := b.BuildAppImage(); err != nil {
			fmt.Printf("  ⚠️  AppImage build failed: %v\n", err)
		}
	}

	// Build Flatpak if manifest exists.
	manifestPath := filepath.Join(b.projectDir, "assets", "linux", b.cfg.App.ID+".yaml")
	if _, err := os.Stat(manifestPath); err == nil {
		if err := b.BuildFlatpak(); err != nil {
			fmt.Printf("  ⚠️  Flatpak build failed: %v\n", err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("some platform builds failed: %w", errors.Join(errs...))
	}
	return nil
}

// BuildFlatpak builds a Flatpak package.
func (b *Builder) BuildFlatpak() error { return b.buildFlatpak() }

// BuildAppImage builds an AppImage package.
func (b *Builder) BuildAppImage() error { return b.buildAppImage() }

// Run builds and runs the app for the given platform.
func (b *Builder) Run(platform string) error {
	switch platform {
	case "android":
		return b.runAndroid()
	case "ios":
		return b.runIOS()
	default:
		return b.runFlutter(platform)
	}
}

func (b *Builder) backendDir() string {
	return filepath.Join(b.projectDir, "backend")
}

func (b *Builder) frontendDir() string {
	return filepath.Join(b.projectDir, "frontend")
}

func (b *Builder) buildDir() string {
	return filepath.Join(b.projectDir, "build")
}

// flutterBuildOutputPaths returns a map of symlink suffixes to Flutter build output paths.
// Desktop platforms return a single entry; Android returns two (apk, aab); iOS returns one.
func (b *Builder) flutterBuildOutputPaths(platform string) map[string]string {
	switch platform {
	case "linux":
		return map[string]string{
			"": filepath.Join(b.frontendDir(), "build", "linux", "x64", "release", "bundle"),
		}
	case "macos":
		return map[string]string{
			"": filepath.Join(b.frontendDir(), "build", "macos", "Build", "Products", "Release"),
		}
	case "windows":
		return map[string]string{
			"": filepath.Join(b.frontendDir(), "build", "windows", "x64", "runner", "Release"),
		}
	case "android":
		return map[string]string{
			"apk": filepath.Join(b.frontendDir(), "build", "app", "outputs", "flutter-apk"),
			"aab": filepath.Join(b.frontendDir(), "build", "app", "outputs", "bundle", "release"),
		}
	case "ios":
		return map[string]string{
			"": filepath.Join(b.frontendDir(), "build", "ios", "ipa"),
		}
	default:
		return nil
	}
}

// linkBuildOutput creates symlinks at build/bin/<platform>/ pointing to the Flutter build outputs.
func (b *Builder) linkBuildOutput(platform string) error {
	paths := b.flutterBuildOutputPaths(platform)
	if len(paths) == 0 {
		return nil
	}

	for suffix, src := range paths {
		link := filepath.Join(b.buildDir(), "bin", platform, suffix)
		if suffix == "" {
			link = filepath.Join(b.buildDir(), "bin", platform)
		}

		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			return err
		}
		os.Remove(link)
		if err := os.Symlink(src, link); err != nil {
			return err
		}
	}

	return nil
}

// runCommand executes a command with combined output on error.
func runCommand(name string, args []string, dir string, env []string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), env...)
	return cmd.Run()
}
