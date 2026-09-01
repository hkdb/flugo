// Package scaffold generates new Flugo project directories from templates.
package scaffold

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/hkdb/flugo/internal/gotoolchain"
)

//go:embed templates/*
var templateFS embed.FS

//go:embed assets/icon.svg
var defaultIconSVG []byte

// Options configures project scaffolding.
type Options struct {
	Name       string // Project name (e.g., "myapp")
	OutputDir  string // Parent directory to create project in
	Module     string // Go module path (e.g., "github.com/user/myapp")
	FlugoPath  string // Path to flugo module (for replace directive)
	CLIVersion string // CLI version to stamp in flugo.yaml
}

// templateData holds all fields available to templates.
type templateData struct {
	Name            string
	DartName        string // Dart-safe name (hyphens replaced with underscores)
	AppName         string
	AppID           string
	Module          string
	FlugoPath       string
	Description     string
	Version         string
	License         string
	URL             string
	Date            string
	Runtime         string
	RuntimeVersion  string
	SDK             string
	Permissions     []string
	WindowWidth     int
	WindowHeight    int
	MinWindowWidth  int
	MinWindowHeight int
	TitlebarStyle   string
	FlugoVersion    string
	URLScheme       string
	GoArches        []gotoolchain.Arch
}

// Create scaffolds a new Flugo project.
// projectNameRe restricts the project name to a safe, cross-platform form. It
// also blocks path traversal: the name is joined into the output path and used
// as filenames (AppID, .desktop, etc.), so "..", "/", "\" must never slip in.
var projectNameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func Create(opts Options) error {
	if opts.Name == "" {
		return fmt.Errorf("project name is required")
	}
	if !projectNameRe.MatchString(opts.Name) {
		return fmt.Errorf("invalid project name %q: use lowercase letters, digits, '-' or '_', starting with a letter", opts.Name)
	}
	if strings.Contains(opts.Module, "..") || strings.ContainsAny(opts.Module, " \t\r\n\\") {
		return fmt.Errorf("invalid module path %q (no '..', whitespace, or backslashes)", opts.Module)
	}

	projectDir := filepath.Join(opts.OutputDir, opts.Name)

	if _, err := os.Stat(projectDir); err == nil {
		return fmt.Errorf("directory %s already exists", projectDir)
	}

	// Resolve FlugoPath: expand ~, require absolute path.
	flugoPath := opts.FlugoPath
	if flugoPath != "" {
		if strings.HasPrefix(flugoPath, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolving home directory: %w", err)
			}
			flugoPath = filepath.Join(home, flugoPath[2:])
		}
		if !filepath.IsAbs(flugoPath) {
			return fmt.Errorf("flugo-path must be an absolute path (got %q)", flugoPath)
		}
	}

	data := templateData{
		Name:            opts.Name,
		DartName:        strings.ReplaceAll(opts.Name, "-", "_"),
		AppName:         toTitle(opts.Name),
		AppID:           deriveAppID(opts.Module, opts.Name),
		Module:          opts.Module,
		FlugoPath:       flugoPath,
		Description:     "A Flugo application",
		Version:         "0.1.0",
		Date:            time.Now().Format("2006-01-02"),
		Runtime:         "org.freedesktop.Platform",
		RuntimeVersion:  "24.08",
		SDK:             "org.freedesktop.Sdk",
		WindowWidth:     1280,
		WindowHeight:    720,
		MinWindowWidth:  360,
		MinWindowHeight: 480,
		TitlebarStyle:   "default",
		FlugoVersion:    opts.CLIVersion,
		GoArches:        gotoolchain.Arches,
	}

	fmt.Println("\n📁 Scaffolding project structure...")

	// Create directory structure.
	dirs := []string{
		"backend/bridge",
		"frontend/lib/bridge",
		"frontend/lib/app",
		"frontend/hook",
		"assets/icons",
		"assets/linux",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(projectDir, d), 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", d, err)
		}
	}

	// Template file groups with section headers.
	type fileGroup struct {
		header string
		files  []struct{ tmpl, output string }
	}
	groups := []fileGroup{
		{
			header: "⚙️  Scaffolding Go backend...",
			files: []struct{ tmpl, output string }{
				{"go_mod.tmpl", "backend/go.mod"},
				{"main_go.tmpl", "backend/main.go"},
				{"service_go.tmpl", "backend/service.go"},
				{"golangci_yml.tmpl", "backend/.golangci.yml"},
			},
		},
		{
			header: "🦋 Scaffolding Flutter frontend...",
			files: []struct{ tmpl, output string }{
				{"pubspec_yaml.tmpl", "frontend/pubspec.yaml"},
				{"main_dart.tmpl", "frontend/lib/main.dart"},
				{"app_dart.tmpl", "frontend/lib/app/app.dart"},
				{"titlebar_dart.tmpl", "frontend/lib/app/titlebar.dart"},
				{"build_dart.tmpl", "frontend/hook/build.dart"},
				{"ffigen_yaml.tmpl", "frontend/ffigen.yaml"},
				{"analysis_options_yaml.tmpl", "frontend/analysis_options.yaml"},
			},
		},
		{
			header: "📦 Scaffolding config & packaging...",
			files: []struct{ tmpl, output string }{
				{"gitignore.tmpl", ".gitignore"},
				{"flugo_yaml.tmpl", "flugo.yaml"},
				{"makefile.tmpl", "Makefile"},
				{"desktop_file.tmpl", fmt.Sprintf("assets/linux/%s.desktop", data.AppID)},
				{"metainfo_xml.tmpl", fmt.Sprintf("assets/linux/%s.metainfo.xml", data.AppID)},
				{"flatpak_manifest.tmpl", fmt.Sprintf("assets/linux/%s.yaml", data.AppID)},
				{"readme.tmpl", "README.md"},
			},
		},
	}

	tmpl, err := parseTemplates()
	if err != nil {
		return err
	}

	for _, group := range groups {
		fmt.Printf("\n%s\n", group.header)
		for _, file := range group.files {
			fullPath := filepath.Join(projectDir, file.output)
			if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
				return err
			}

			f, err := os.Create(fullPath)
			if err != nil {
				return fmt.Errorf("creating %s: %w", file.output, err)
			}

			if err := tmpl.ExecuteTemplate(f, file.tmpl, data); err != nil {
				f.Close()
				return fmt.Errorf("executing template %s: %w", file.tmpl, err)
			}
			f.Close()
			fmt.Printf("  ✅ %s\n", file.output)
		}
	}

	fmt.Println("\n🔧 Generating Flutter platform runners...")

	// Generate Flutter platform runners for all platforms.
	frontendDir := filepath.Join(projectDir, "frontend")
	flutterCmd := exec.Command("flutter", "create", "--project-name", data.DartName, "--platforms", "linux,macos,windows,android,ios", ".")
	flutterCmd.Dir = frontendDir
	if out, err := flutterCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("flutter create: %w\n%s", err, out)
	}
	fmt.Println("  ✅ linux, macos, windows, android, ios")

	// Patch platform entitlements/permissions for file access.
	if err := patchPlatformPermissions(frontendDir); err != nil {
		fmt.Printf("  ⚠️  Could not patch platform permissions: %v\n", err)
	}

	// Patch Android MainActivity to include System.loadLibrary("backend")
	// so JNI_OnLoad is called and the JVM pointer is available for Go code.
	if err := patchAndroidManifestPermissions(frontendDir); err != nil {
		return err
	}
	if err := patchAndroidMainActivity(frontendDir); err != nil {
		fmt.Printf("  ⚠️  Could not patch Android MainActivity: %v\n", err)
	}

	// Patch Linux CMakeLists.txt to use the correct APPLICATION_ID
	// (flutter create defaults to com.example.{dart_name}).
	if err := patchLinuxApplicationID(frontendDir, data.AppID); err != nil {
		fmt.Printf("  ⚠️  Could not patch Linux APPLICATION_ID: %v\n", err)
	}

	// Write default Flugo icon.
	iconPath := filepath.Join(projectDir, "assets/icons/icon.svg")
	if err := os.WriteFile(iconPath, defaultIconSVG, 0o644); err != nil {
		return fmt.Errorf("creating assets/icons/icon.svg: %w", err)
	}

	// Create empty placeholder files.
	placeholders := []string{
		"frontend/lib/bridge/.gitkeep",
	}
	for _, p := range placeholders {
		path := filepath.Join(projectDir, p)
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			return fmt.Errorf("creating %s: %w", p, err)
		}
	}

	return nil
}

// patchPlatformPermissions adds required entitlements and permissions to
// platform runner files generated by flutter create.
func patchPlatformPermissions(frontendDir string) error {
	// macOS: disable sandbox (default), add file access and network entitlements
	for _, entFile := range []string{
		"macos/Runner/Release.entitlements",
		"macos/Runner/DebugProfile.entitlements",
	} {
		path := filepath.Join(frontendDir, entFile)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(data)
		// Disable sandbox by default (can be re-enabled via flugo.yaml macos.sandbox)
		content = strings.Replace(content,
			"<key>com.apple.security.app-sandbox</key>\n\t<true/>",
			"<key>com.apple.security.app-sandbox</key>\n\t<false/>",
			1)
		// Add file access and network entitlements
		if !strings.Contains(content, "files.user-selected.read-write") {
			content = strings.Replace(content,
				"</dict>",
				"\t<key>com.apple.security.files.user-selected.read-write</key>\n\t<true/>\n\t<key>com.apple.security.network.client</key>\n\t<true/>\n</dict>",
				1)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("patching %s: %w", entFile, err)
		}
	}

	// iOS: add photo library usage description
	iosInfoPlist := filepath.Join(frontendDir, "ios/Runner/Info.plist")
	data, err := os.ReadFile(iosInfoPlist)
	if err == nil {
		content := string(data)
		if !strings.Contains(content, "NSPhotoLibraryUsageDescription") {
			content = strings.Replace(content,
				"</dict>\n</plist>",
				"\t<key>NSPhotoLibraryUsageDescription</key>\n\t<string>Photo library access is needed to select files</string>\n</dict>\n</plist>",
				1)
			_ = os.WriteFile(iosInfoPlist, []byte(content), 0o644)
		}
	}

	fmt.Println("  ✅ Patched platform permissions")
	return nil
}

// parseTemplates parses all embedded template files and returns the template set.
func parseTemplates() (*template.Template, error) {
	tmpl := template.New("")
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		return nil, fmt.Errorf("reading embedded templates: %w", err)
	}
	for _, entry := range entries {
		content, err := templateFS.ReadFile("templates/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("reading template %s: %w", entry.Name(), err)
		}
		if _, err = tmpl.New(entry.Name()).Parse(string(content)); err != nil {
			return nil, fmt.Errorf("parsing template %s: %w", entry.Name(), err)
		}
	}
	return tmpl, nil
}

// deriveAppID converts a Go module path into a reverse-domain app ID.
// e.g. "github.com/instacryptio/ic-app" → "io.github.instacryptio.ic-app"
// Falls back to "com.example.<name>" if module is empty or unparseable.
func deriveAppID(module, name string) string {
	if module == "" {
		return "com.example." + name
	}

	parts := strings.Split(module, "/")
	if len(parts) < 2 {
		return "com.example." + name
	}

	// Reverse the domain part (e.g. "github.com" → "com.github").
	domainParts := strings.Split(parts[0], ".")
	for i, j := 0, len(domainParts)-1; i < j; i, j = i+1, j-1 {
		domainParts[i], domainParts[j] = domainParts[j], domainParts[i]
	}

	// Combine: reversed domain + remaining path segments.
	segments := append(domainParts, parts[1:]...)
	return strings.Join(segments, ".")
}

func toTitle(s string) string {
	if s == "" {
		return s
	}
	// Convert kebab-case or snake_case to Title Case.
	parts := strings.FieldsFunc(s, func(c rune) bool {
		return c == '-' || c == '_'
	})
	for i, p := range parts {
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// patchAndroidManifestPermissions adds the INTERNET permission to the main
// AndroidManifest. Flutter's scaffold only grants it in the debug/profile
// manifests (for hot reload), so without this patch every networked flugo
// app resolves no hosts in release builds ("no such host" from the Go
// layer). Idempotent — safe from both scaffold and `flugo update`.
func patchAndroidManifestPermissions(frontendDir string) error {
	path := filepath.Join(frontendDir, "android", "app", "src", "main", "AndroidManifest.xml")
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // android platform not generated
	}
	if err != nil {
		return fmt.Errorf("reading AndroidManifest.xml: %w", err)
	}
	content := string(raw)
	if strings.Contains(content, "android.permission.INTERNET") {
		return nil
	}
	idx := strings.Index(content, ">")
	if idx < 0 {
		return fmt.Errorf("AndroidManifest.xml has an unexpected layout")
	}
	content = content[:idx+1] + "\n    <uses-permission android:name=\"android.permission.INTERNET\"/>" + content[idx+1:]
	return os.WriteFile(path, []byte(content), 0o644)
}

// errNoAndroidMainActivity is returned by renderMainActivity when the project
// has no Android MainActivity.kt (e.g. a desktop-only app), so callers can skip
// it rather than fail.
var errNoAndroidMainActivity = errors.New("no android MainActivity.kt")

// renderMainActivity locates the app's MainActivity.kt, extracts its package,
// and renders flugo's mainactivity_kt.tmpl (System.loadLibrary("backend") so
// JNI_OnLoad runs + the SAF "Open Folder" MethodChannel) for that package. It
// returns the file path and the rendered bytes WITHOUT writing, so both scaffold
// (create) and Update (refresh) drive the one source of truth — flugo owns this
// file, apps never hand-patch it. Returns errNoAndroidMainActivity when there is
// no Android MainActivity to manage.
func renderMainActivity(frontendDir string) (path string, content []byte, err error) {
	// Flutter places it at android/app/src/main/kotlin/{package_path}/MainActivity.kt
	kotlinDir := filepath.Join(frontendDir, "android", "app", "src", "main", "kotlin")
	if _, statErr := os.Stat(kotlinDir); statErr != nil {
		return "", nil, errNoAndroidMainActivity
	}
	walkErr := filepath.Walk(kotlinDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			//nolint:nilerr // filepath.Walk: returning nil = "skip this entry, continue walking"
			return nil
		}
		if info.Name() == "MainActivity.kt" {
			path = p
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return "", nil, fmt.Errorf("walking kotlin dir %s: %w", kotlinDir, walkErr)
	}
	if path == "" {
		return "", nil, errNoAndroidMainActivity
	}

	existing, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("reading MainActivity.kt: %w", err)
	}
	var packageName string
	for _, line := range strings.Split(string(existing), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package ") {
			packageName = strings.TrimPrefix(line, "package ")
			break
		}
	}
	if packageName == "" {
		return "", nil, fmt.Errorf("could not extract package name from MainActivity.kt")
	}

	tmpl, err := parseTemplates()
	if err != nil {
		return "", nil, fmt.Errorf("parsing templates: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "mainactivity_kt.tmpl", struct{ AndroidPackage string }{packageName}); err != nil {
		return "", nil, fmt.Errorf("rendering MainActivity.kt: %w", err)
	}
	return path, buf.Bytes(), nil
}

// patchAndroidMainActivity writes flugo's SAF MainActivity at create time.
func patchAndroidMainActivity(frontendDir string) error {
	path, content, err := renderMainActivity(frontendDir)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("writing MainActivity.kt: %w", err)
	}
	fmt.Println("  ✅ Android MainActivity written (System.loadLibrary + SAF Open Folder)")
	return nil
}

// patchLinuxApplicationID replaces the default com.example.{dart_name}
// APPLICATION_ID in CMakeLists.txt with the correct app ID from flugo.yaml.
func patchLinuxApplicationID(frontendDir, appID string) error {
	cmakePath := filepath.Join(frontendDir, "linux", "CMakeLists.txt")
	content, err := os.ReadFile(cmakePath)
	if err != nil {
		return fmt.Errorf("reading CMakeLists.txt: %w", err)
	}

	original := string(content)
	var lines []string
	for _, line := range strings.Split(original, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "set(APPLICATION_ID") {
			lines = append(lines, fmt.Sprintf(`set(APPLICATION_ID "%s")`, appID))
			continue
		}
		lines = append(lines, line)
	}
	patched := strings.Join(lines, "\n")

	if patched == original {
		return nil
	}

	if err := os.WriteFile(cmakePath, []byte(patched), 0o644); err != nil {
		return fmt.Errorf("writing patched CMakeLists.txt: %w", err)
	}
	fmt.Println("  ✅ Linux APPLICATION_ID patched")
	return nil
}
