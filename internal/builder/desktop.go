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

	// `flutter build` triggers the native-assets hook (hook/build.dart), which
	// would compile the backend a second time — wasted work, since buildGoShared
	// above already produced the library bundleGoLib ships. Drop a marker the
	// hook checks so it skips its own `go build`. The hooks runner strips env
	// vars (only PATH survives), so a marker file is used rather than an env
	// flag. Removed afterwards so a later `flutter run` (dev) still builds via
	// the hook as usual.
	skipMarker := filepath.Join(b.frontendDir(), ".flugo-skip-backend-hook")
	if err := os.WriteFile(skipMarker, nil, 0o644); err != nil {
		return fmt.Errorf("writing backend-hook skip marker: %w", err)
	}
	defer os.Remove(skipMarker)

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
		// Locate the actual .app Flutter built rather than recomputing its
		// name: Flutter names the bundle after PRODUCT_NAME (AppInfo.xcconfig),
		// which may differ from the project dir (e.g. a branded PRODUCT_NAME).
		// Recomputing the name would MkdirAll a wrong path and fabricate a
		// hollow bundle containing only the Go lib; globbing the real bundle
		// keeps the lib landing in the populated app.
		releaseDir := filepath.Join(b.frontendDir(), "build", "macos", "Build", "Products", "Release")
		apps, err := filepath.Glob(filepath.Join(releaseDir, "*.app"))
		if err != nil {
			return err
		}
		appPath := ""
		for _, a := range apps {
			// A real Flutter bundle has Contents/MacOS; skip any hollow
			// leftover (e.g. from a prior buggy build).
			fi, statErr := os.Stat(filepath.Join(a, "Contents", "MacOS"))
			if statErr != nil || !fi.IsDir() {
				continue
			}
			if appPath != "" {
				return fmt.Errorf("multiple .app bundles in %s; run `flutter clean` and rebuild", releaseDir)
			}
			appPath = a
		}
		if appPath == "" {
			return fmt.Errorf("no built .app found in %s; did `flutter build macos` succeed?", releaseDir)
		}
		dst = filepath.Join(appPath, "Contents", "Frameworks", outputName)
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

	if err := os.WriteFile(dst, data, 0o755); err != nil {
		return err
	}
	fmt.Printf("  📦 Bundled %s\n", outputName)

	// macOS: the Go library links its C dependencies (e.g. libfido2 → libcbor,
	// libcrypto) dynamically by their absolute Homebrew install names, so a
	// bundle copied to another Mac fails to load them. Copy that dependency
	// closure into Contents/Frameworks and rewrite the load paths to
	// @loader_path so the .app is self-contained. (Linux/Windows keep the plain
	// copy — Windows dep-bundling is handled downstream in CI.)
	if platform == "macos" {
		if err := b.makeMacAppSelfContained(filepath.Dir(dst), dst); err != nil {
			return fmt.Errorf("making .app self-contained: %w", err)
		}
	}

	return nil
}

// makeMacAppSelfContained walks the Mach-O dependency graph of the freshly
// bundled Go library and copies every non-system dynamic dependency into
// frameworksDir, rewriting each reference to @loader_path so the .app loads on
// a Mac without the original (e.g. Homebrew) libraries installed. It is the
// macOS analog of macdeployqt: copy the transitive closure, fix install names,
// re-sign. mainDylib is the just-copied library (an absolute path inside
// frameworksDir).
func (b *Builder) makeMacAppSelfContained(frameworksDir, mainDylib string) error {
	isSystem := func(p string) bool {
		return strings.HasPrefix(p, "/usr/lib/") || strings.HasPrefix(p, "/System/Library/")
	}

	bundled := map[string]bool{} // basenames already copied into frameworksDir
	queue := []string{mainDylib}
	total := 0

	for len(queue) > 0 {
		file := queue[0]
		queue = queue[1:]

		deps, err := machODeps(file)
		if err != nil {
			return err
		}

		edited := false
		for _, dep := range deps {
			// System libraries stay dynamic; already-relocated references
			// (@rpath/@loader_path/@executable_path) need no change.
			if isSystem(dep) || strings.HasPrefix(dep, "@") {
				continue
			}
			base := filepath.Base(dep)
			// Skip a dylib's own id (self-reference).
			if base == filepath.Base(file) {
				continue
			}

			if !bundled[base] {
				bundled[base] = true
				total++
				copyDst := filepath.Join(frameworksDir, base)
				if err := copyFile(dep, copyDst, 0o755); err != nil {
					return fmt.Errorf("bundling %s: %w", dep, err)
				}
				if err := runCommand("install_name_tool", []string{"-id", "@loader_path/" + base, copyDst}, "", nil); err != nil {
					return fmt.Errorf("setting id of %s: %w", base, err)
				}
				if err := codesignAdhoc(copyDst); err != nil {
					return err
				}
				// Process this dependency's own dependencies too.
				queue = append(queue, copyDst)
			}

			// Rewrite the reference in the current file to the bundled copy.
			if err := runCommand("install_name_tool", []string{"-change", dep, "@loader_path/" + base, file}, "", nil); err != nil {
				return fmt.Errorf("rewriting %s in %s: %w", dep, filepath.Base(file), err)
			}
			edited = true
		}

		// install_name_tool invalidates the code signature; an invalid
		// signature is fatal on Apple Silicon, so re-sign ad-hoc after edits.
		if edited {
			if err := codesignAdhoc(file); err != nil {
				return err
			}
		}
	}

	// Guardrail: fail loudly if any non-system, non-relocated dependency of the
	// main library survived the rewrite (e.g. a copy that silently failed).
	deps, err := machODeps(mainDylib)
	if err != nil {
		return err
	}
	for _, dep := range deps {
		if !isSystem(dep) && !strings.HasPrefix(dep, "@") && filepath.Base(dep) != filepath.Base(mainDylib) {
			return fmt.Errorf("%s still references non-system library %s", filepath.Base(mainDylib), dep)
		}
	}

	fmt.Printf("  📦 Made .app self-contained (%d libs bundled)\n", total)

	// Adding/rewriting dylibs under Contents/Frameworks after Flutter signed the
	// bundle invalidates the .app's own signature (its CodeResources seal no
	// longer matches — `codesign --verify` reports "a sealed resource is missing
	// or invalid"). An invalid signature destabilizes the app's code identity,
	// which macOS keys TCC grants (e.g. Input Monitoring, needed for HID
	// hardware-key access) to — so the app silently loses those grants. Re-seal
	// the whole bundle LAST (inside-out: the dylibs above were already signed),
	// preserving Flutter's entitlements/flags on the main executable.
	appPath := filepath.Dir(filepath.Dir(frameworksDir)) // <app>/Contents/Frameworks -> <app>
	if err := codesignAppBundle(appPath); err != nil {
		return err
	}
	fmt.Println("  🔏 Re-sealed .app signature")
	return nil
}

// machODeps returns the dynamic-library dependencies recorded in a Mach-O file
// via `otool -L`, dropping the leading header line. The file's own LC_ID_DYLIB
// (a self-reference) is left in the list; callers skip it by basename.
func machODeps(file string) ([]string, error) {
	out, err := exec.Command("otool", "-L", file).Output()
	if err != nil {
		return nil, fmt.Errorf("otool -L %s: %w", filepath.Base(file), err)
	}
	var deps []string
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if i == 0 { // "<file>:" header
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Each dep line is "<path> (compatibility version …, current version …)".
		if idx := strings.Index(line, " ("); idx != -1 {
			line = line[:idx]
		}
		deps = append(deps, strings.TrimSpace(line))
	}
	return deps, nil
}

// codesignAdhoc applies an ad-hoc (unsigned-identity) code signature. Required
// after install_name_tool edits so the dylib remains loadable on Apple Silicon;
// a later Developer-ID + notarization pass re-signs over this.
func codesignAdhoc(file string) error {
	if err := runCommand("codesign", []string{"--force", "--sign", "-", file}, "", nil); err != nil {
		return fmt.Errorf("ad-hoc signing %s: %w", filepath.Base(file), err)
	}
	return nil
}

// codesignAppBundle re-seals an .app bundle ad-hoc so its CodeResources seal
// covers the dylibs we added under Contents/Frameworks. --preserve-metadata
// keeps the main executable's existing entitlements/flags/runtime from Flutter's
// signature so re-signing doesn't strip them.
func codesignAppBundle(appPath string) error {
	args := []string{
		"--force", "--sign", "-",
		"--preserve-metadata=entitlements,requirements,flags,runtime",
		appPath,
	}
	if err := runCommand("codesign", args, "", nil); err != nil {
		return fmt.Errorf("re-sealing %s: %w", filepath.Base(appPath), err)
	}
	return nil
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

	args := b.goBuildArgs("c-shared", outputPath)

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
