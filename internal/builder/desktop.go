package builder

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// macSandboxRe matches the app-sandbox entitlement key and its boolean value,
// tolerating whitespace between the key and value so the value can be flipped
// without disturbing formatting.
var macSandboxRe = regexp.MustCompile(`(<key>com\.apple\.security\.app-sandbox</key>\s*)<(?:true|false)/>`)

// applyMacOSSandbox sets the macOS app-sandbox entitlement in both entitlements
// files to match flugo.yaml's platforms.macos.sandbox. Missing files (macOS
// runner not scaffolded on this host) are skipped silently.
func (b *Builder) applyMacOSSandbox(enabled bool) error {
	val := "<false/>"
	if enabled {
		val = "<true/>"
	}
	for _, entFile := range []string{
		"macos/Runner/Release.entitlements",
		"macos/Runner/DebugProfile.entitlements",
	} {
		path := filepath.Join(b.frontendDir(), entFile)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(data)
		if !macSandboxRe.MatchString(content) {
			continue
		}
		updated := macSandboxRe.ReplaceAllString(content, "${1}"+val)
		if updated == content {
			continue
		}
		if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
			return fmt.Errorf("setting macOS sandbox in %s: %w", entFile, err)
		}
	}
	return nil
}

// buildDesktop builds the Go shared library and Flutter app for a desktop platform.
func (b *Builder) buildDesktop(platform string, release bool) error {
	if platform == "macos" {
		if err := b.applyMacOSSandbox(b.cfg.Platforms.MacOS.Sandbox); err != nil {
			return fmt.Errorf("applying macOS sandbox setting: %w", err)
		}
	}

	if err := b.buildGoShared(platform); err != nil {
		return fmt.Errorf("go build: %w", err)
	}

	if err := b.buildFlutter(platform, release); err != nil {
		return fmt.Errorf("flutter build: %w", err)
	}

	// Copy the Go library into the platform-specific output location
	if err := b.bundleGoLib(platform); err != nil {
		return fmt.Errorf("bundling Go library: %w", err)
	}

	return nil
}

// bundleGoLib copies the compiled Go shared library into the correct location
// within the Flutter build output so it can be found at runtime.
func (b *Builder) bundleGoLib(platform string) error {
	_, _, _, outputName := desktopBuildParams(platform)
	src := filepath.Join(b.buildDir(), platform, outputName)

	var dst string
	switch platform {
	case "macos":
		// Find the .app bundle in the build output
		appName := strings.ReplaceAll(filepath.Base(b.projectDir), "-", "_")
		dst = filepath.Join(b.frontendDir(), "build", "macos", "Build", "Products", "Release", appName+".app", "Contents", "Frameworks", outputName)
	default:
		// Linux/Windows: library goes next to the executable in the bundle
		paths := b.flutterBuildOutputPaths(platform)
		for _, p := range paths {
			dst = filepath.Join(p, outputName)
			break
		}
	}

	if dst == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("reading %s: %w", src, err)
	}

	fmt.Printf("  📦 Bundled %s\n", outputName)
	return os.WriteFile(dst, data, 0o755)
}

// buildGoShared compiles the Go backend as a C shared library.
func (b *Builder) buildGoShared(platform string) error {
	outDir := filepath.Join(b.buildDir(), platform)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	goos, goarch, cc, outputName := desktopBuildParams(platform)

	outputPath := filepath.Join(outDir, outputName)

	env := []string{
		"CGO_ENABLED=1",
		fmt.Sprintf("GOOS=%s", goos),
		fmt.Sprintf("GOARCH=%s", goarch),
	}
	if cc != "" {
		env = append(env, fmt.Sprintf("CC=%s", cc))
	}
	// macOS: set SDKROOT so the linker finds SDK libraries (e.g. libresolv)
	if goos == "darwin" {
		out, err := exec.Command("xcrun", "--show-sdk-path").Output()
		if err == nil {
			env = append(env, fmt.Sprintf("SDKROOT=%s", strings.TrimSpace(string(out))))
		}
	}

	args := []string{"build", "-buildmode=c-shared", "-o", outputPath, "."}

	fmt.Printf("  ⚙️  Compiling Go backend (%s/%s)...\n", goos, goarch)
	return runCommand("go", args, b.backendDir(), env)
}

// desktopBuildParams returns GOOS, GOARCH, CC, and output filename for a platform.
func desktopBuildParams(platform string) (goos, goarch, cc, outputName string) {
	hostArch := runtime.GOARCH

	switch platform {
	case "linux":
		goos = "linux"
		goarch = hostArch
		outputName = "libbackend.so"
		// hostArch == "arm64": building natively works without a cross-compiler;
		// building amd64 → arm64 would need one but is not handled here.
	case "macos":
		goos = "darwin"
		goarch = hostArch
		outputName = "libbackend.dylib"
	case "windows":
		goos = "windows"
		goarch = "amd64"
		outputName = "backend.dll"
		// Cross-compile from Linux/macOS to Windows.
		if runtime.GOOS != "windows" {
			cc = "x86_64-w64-mingw32-gcc"
		}
	}
	return
}

// buildFlutter runs `flutter build` for the target desktop platform.
func (b *Builder) buildFlutter(platform string, release bool) error {
	flutterPlatform := platform
	if platform == "macos" {
		flutterPlatform = "macos"
	}

	args := []string{"build", flutterPlatform}
	if release {
		args = append(args, "--release")
	}

	fmt.Printf("  🦋 Building Flutter app (%s)...\n", platform)
	return runCommand("flutter", args, b.frontendDir(), nil)
}

// runFlutter runs `flutter run` for local development.
func (b *Builder) runFlutter(platform string) error {
	if platform == "macos" {
		if err := b.applyMacOSSandbox(b.cfg.Platforms.MacOS.Sandbox); err != nil {
			return fmt.Errorf("applying macOS sandbox setting: %w", err)
		}
	}

	args := []string{"run", "-d", flutterDeviceID(platform)}
	fmt.Printf("  🚀 Running Flutter app (%s)...\n", platform)
	return runCommand("flutter", args, b.frontendDir(), nil)
}

func flutterDeviceID(platform string) string {
	switch platform {
	case "linux":
		return "linux"
	case "macos":
		return "macos"
	case "windows":
		return "windows"
	}
	return platform
}
