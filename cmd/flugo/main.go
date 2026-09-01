package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/hkdb/flugo/internal/util"

	"github.com/hkdb/flugo/internal/appimage"
	"github.com/hkdb/flugo/internal/builder"
	"github.com/hkdb/flugo/internal/codegen"
	"github.com/hkdb/flugo/internal/config"
	"github.com/hkdb/flugo/internal/flathub"
	"github.com/hkdb/flugo/internal/packager"
	"github.com/hkdb/flugo/internal/scaffold"
	"github.com/spf13/cobra"
)

var version = "0.1.0"

func main() {
	if err := rootCmd.Execute(); err != nil {
		util.PrintError(err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:           "flugo",
	Short:         "Flugo -- Go + Flutter app framework",
	Long:          "Flugo makes building cross-platform Go + Flutter apps dead easy.",
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		util.PrintBanner()
	},
	Run: func(cmd *cobra.Command, args []string) {
		if v, _ := cmd.Flags().GetBool("version"); v {
			fmt.Printf("flugo %s\n", version)
			return
		}
		_ = cmd.Help()
	},
}

func init() {
	rootCmd.Flags().BoolP("version", "v", false, "Print Flugo version")
	lintCmd.Flags().Bool("fix", false, "Apply auto-fixes (golangci-lint --fix, dart fix --apply, dart format)")
	rootCmd.AddCommand(
		createCmd,
		generateCmd,
		buildCmd,
		runCmd,
		updateCmd,
		upgradeCmd,
		deeplinkCmd,
		packageCmd,
		flathubCmd,
		appimageCmd,
		iconsCmd,
		doctorCmd,
		lintCmd,
		cleanCmd,
		versionCmd,
	)
}

// --- create ---

var createCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Scaffold a new Flugo project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		module, _ := cmd.Flags().GetString("module")
		if module == "" {
			module = name
		}

		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		// Optional: local flugo path for replace directive (dev only).
		flugoPath, _ := cmd.Flags().GetString("flugo-path")

		opts := scaffold.Options{
			Name:       name,
			OutputDir:  cwd,
			Module:     module,
			FlugoPath:  flugoPath,
			CLIVersion: version,
		}

		if err := scaffold.Create(opts); err != nil {
			return err
		}

		fmt.Println()
		util.PrintSuccess("🎉 Created project:", name+"/")
		fmt.Println()
		util.PrintSuccess("👉 Next steps:", "")
		fmt.Printf("  cd %s\n", name)
		fmt.Println("  flugo generate")
		fmt.Println("  flugo run")
		return nil
	},
}

func init() {
	createCmd.Flags().StringP("module", "m", "", "Go module path (default: <name>)")
	createCmd.Flags().String("flugo-path", "", "Path to local flugo module (for development)")
}

// --- update ---

var deeplinkCmd = &cobra.Command{
	Use:   "deeplink",
	Short: "Register app.url_scheme deep linking across the platform files",
	Long: `Register the flugo.yaml app.url_scheme as a deep-link handler:
patches the Android manifest and iOS/macOS Info.plists (idempotent) and
re-renders the Linux .desktop entry. The Dart-side wiring lands in main.dart
on the next 'flugo update'/'flugo run'; on Windows the app registers its
HKCU handler at startup via bridge.RegisterURLScheme.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, err := findProject()
		if err != nil {
			return err
		}
		cfg, err := config.Load(projectDir)
		if err != nil {
			return err
		}
		fmt.Println("\n🔗 Registering deep-link scheme...")
		if err := scaffold.ApplyDeepLink(projectDir, cfg, version); err != nil {
			return err
		}
		fmt.Println("\nDone. Remember:")
		fmt.Println("  - add app_links to frontend/pubspec.yaml (new scaffolds get it automatically)")
		fmt.Println("  - run 'flugo update' so main.dart picks up the deep-link wiring")
		fmt.Println("  - call bridge.RegisterURLScheme(\"" + cfg.App.URLScheme + "\") in backend main() for Windows")
		return nil
	},
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update framework files from latest templates",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, err := findProject()
		if err != nil {
			return err
		}

		cfg, err := config.Load(projectDir)
		if err != nil {
			return err
		}

		all, _ := cmd.Flags().GetBool("all")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if all && !dryRun {
			fmt.Println("\n⚠️  --all will overwrite user-owned files (app.dart, pubspec.yaml, main.go, etc.)")
			fmt.Println("   Backups (.bak) will be created for any changed files.")

			force, _ := cmd.Flags().GetBool("force")
			if !force {
				fmt.Print("\n   Continue? [y/N] ")
				var answer string
				_, _ = fmt.Scanln(&answer)
				if answer != "y" && answer != "Y" {
					fmt.Println("Aborted.")
					return nil
				}
			}
		}

		fmt.Println()
		if dryRun {
			fmt.Print("🔍 Dry run — no files will be written.\n\n")
		}

		if cfg.FlugoVersion != "" && cfg.FlugoVersion != version && !dryRun {
			fmt.Printf("  ℹ️  Updating flugo_version: %s → %s\n\n", cfg.FlugoVersion, version)
		}

		result, err := scaffold.Update(projectDir, cfg, all, dryRun, version)
		if err != nil {
			return err
		}

		if len(result.Created) == 0 && len(result.Updated) == 0 {
			fmt.Println("✅ All framework files are already up to date.")
			return nil
		}

		for _, f := range result.Created {
			fmt.Printf("  ✨ created  %s\n", f)
		}
		for _, f := range result.Updated {
			if dryRun {
				fmt.Printf("  📝 would update  %s\n", f)
				continue
			}
			fmt.Printf("  📝 updated  %s\n", f)
		}
		for _, f := range result.Backed {
			fmt.Printf("  💾 backup   %s\n", f)
		}
		if len(result.Skipped) > 0 {
			fmt.Printf("\n  ⏭️  %d file(s) unchanged\n", len(result.Skipped))
		}

		if !dryRun && (len(result.Created) > 0 || len(result.Updated) > 0) {
			fmt.Println("\n🧹 Cleaning Flutter build cache...")
			frontendDir := filepath.Join(projectDir, "frontend")
			cleanCmd := exec.Command("flutter", "clean")
			cleanCmd.Dir = frontendDir
			cleanCmd.Stdout = os.Stdout
			cleanCmd.Stderr = os.Stderr
			if err := cleanCmd.Run(); err != nil {
				fmt.Printf("  ⚠️  flutter clean failed: %v\n", err)
			}
		}

		return nil
	},
}

func init() {
	updateCmd.Flags().Bool("all", false, "Also update user-owned files (app.dart, pubspec.yaml, go files, etc.)")
	updateCmd.Flags().Bool("dry-run", false, "Preview changes without writing files")
	updateCmd.Flags().BoolP("force", "y", false, "Skip confirmation prompt for --all")
}

// --- generate ---

var generateCmd = &cobra.Command{
	Use:     "generate",
	Aliases: []string{"gen"},
	Short:   "Regenerate bridge code from bound Go structs",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, err := findProject()
		if err != nil {
			return err
		}

		cfg, err := config.Load(projectDir)
		if err != nil {
			return err
		}

		frontendDir := filepath.Join(projectDir, "frontend")
		backendDir := filepath.Join(projectDir, "backend")
		goOutputDir := filepath.Join(backendDir, "bridge")
		dartOutputDir := filepath.Join(frontendDir, "lib", "bridge")

		regenPlatforms, _ := cmd.Flags().GetBool("regen-platforms")
		if regenPlatforms {
			dartName := strings.ReplaceAll(cfg.App.Name, "-", "_")
			dartName = strings.ReplaceAll(strings.ToLower(dartName), " ", "_")
			fmt.Println("\n🔧 Regenerating Flutter platform runners...")
			flutterCmd := exec.Command("flutter", "create", "--project-name", dartName, "--platforms", "linux,macos,windows,android,ios", ".")
			flutterCmd.Dir = frontendDir
			if out, err := flutterCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("flutter create: %w\n%s", err, out)
			}
			fmt.Println("  ✅ linux, macos, windows, android, ios")
		}

		fmt.Println("\n🔗 Generating bridge code...")
		gen := codegen.New(backendDir, cfg.Backend.Module)
		if err := gen.Generate(goOutputDir, dartOutputDir); err != nil {
			return err
		}

		fmt.Println("  ✅ backend/bridge.gen.go")
		fmt.Println("  ✅ backend/bridge/bridge.gen.h")
		fmt.Println("  ✅ frontend/lib/bridge/bridge.gen.dart")

		fmt.Println("\n📦 Resolving Go dependencies...")
		tidyCmd := exec.Command("go", "mod", "tidy")
		tidyCmd.Dir = backendDir
		if out, err := tidyCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("go mod tidy: %w\n%s", err, out)
		}
		dlCmd := exec.Command("go", "mod", "download", "all")
		dlCmd.Dir = backendDir
		if out, err := dlCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("go mod download: %w\n%s", err, out)
		}
		fmt.Println("  ✅ go.mod and go.sum updated")

		return nil
	},
}

func init() {
	generateCmd.Flags().Bool("regen-platforms", false, "Regenerate Flutter platform runners (useful after Flutter upgrades)")
}

// --- build ---

var buildCmd = &cobra.Command{
	Use:   "build <platform>",
	Short: "Build for target platform (linux/appimage/flatpak/macos/windows/android/ios/all)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, err := findProject()
		if err != nil {
			return err
		}

		cfg, err := config.Load(projectDir)
		if err != nil {
			return err
		}

		if cfg.FlugoVersion != "" && cfg.FlugoVersion != version {
			fmt.Printf("\n  ℹ️  Project was last updated with flugo %s. Run 'flugo update' to sync framework files.\n\n", cfg.FlugoVersion)
		}

		if err := scaffold.SyncConfigFiles(projectDir, cfg, version); err != nil {
			return fmt.Errorf("syncing config files: %w", err)
		}

		b := builder.New(projectDir, cfg)
		release, _ := cmd.Flags().GetBool("release")

		platform := args[0]
		switch platform {
		case "all":
			return b.BuildAll(release)
		case "flatpak":
			return b.BuildFlatpak()
		case "appimage":
			return b.BuildAppImage()
		default:
			return b.Build(platform, release)
		}
	},
}

func init() {
	buildCmd.Flags().Bool("release", false, "Build in release mode")
}

// --- run ---

var runCmd = &cobra.Command{
	Use:   "run [platform]",
	Short: "Build and run (default: current desktop platform, or specify android/ios)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, err := findProject()
		if err != nil {
			return err
		}

		cfg, err := config.Load(projectDir)
		if err != nil {
			return err
		}

		if cfg.FlugoVersion != "" && cfg.FlugoVersion != version {
			fmt.Printf("\n  ℹ️  Project was last updated with flugo %s. Run 'flugo update' to sync framework files.\n\n", cfg.FlugoVersion)
		}

		if err := scaffold.SyncConfigFiles(projectDir, cfg, version); err != nil {
			return fmt.Errorf("syncing config files: %w", err)
		}

		b := builder.New(projectDir, cfg)

		// If platform specified, use it
		if len(args) > 0 {
			return b.Run(args[0])
		}

		// Auto-detect host desktop platform
		switch runtime.GOOS {
		case "linux":
			return b.Run("linux")
		case "darwin":
			return b.Run("macos")
		case "windows":
			return b.Run("windows")
		default:
			return fmt.Errorf("unsupported platform for run: %s (try: flugo run android|ios)", runtime.GOOS)
		}
	},
}

// --- package ---

var packageCmd = &cobra.Command{
	Use:   "package <platform>",
	Short: "Create distributable package",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, err := findProject()
		if err != nil {
			return err
		}

		cfg, err := config.Load(projectDir)
		if err != nil {
			return err
		}

		p := packager.New(projectDir, cfg)
		return p.Package(args[0])
	},
}

// --- flathub ---

var flathubCmd = &cobra.Command{
	Use:   "flathub",
	Short: "Generate Flathub submission assets",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, err := findProject()
		if err != nil {
			return err
		}

		cfg, err := config.Load(projectDir)
		if err != nil {
			return err
		}

		return flathub.Generate(projectDir, cfg)
	},
}

// --- appimage ---

var appimageCmd = &cobra.Command{
	Use:   "appimage",
	Short: "Generate AppImage packaging assets",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, err := findProject()
		if err != nil {
			return err
		}

		cfg, err := config.Load(projectDir)
		if err != nil {
			return err
		}

		return appimage.Generate(projectDir, cfg)
	},
}

// --- icons ---

var iconsCmd = &cobra.Command{
	Use:   "icons",
	Short: "Generate platform icons from source SVG",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, err := findProject()
		if err != nil {
			return err
		}

		cfg, err := config.Load(projectDir)
		if err != nil {
			return err
		}

		return generateIcons(projectDir, cfg)
	},
}

func generateIcons(projectDir string, cfg *config.Config) error {
	source := filepath.Join(projectDir, cfg.Icons.Source)
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("source icon not found: %s", source)
	}

	renderIcon, err := findIconRenderer()
	if err != nil {
		return err
	}

	// Standard icons
	outputDir := filepath.Join(projectDir, "assets", "icons")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	for _, size := range []int{64, 128, 256, 512} {
		out := filepath.Join(outputDir, fmt.Sprintf("icon-%dx%d.png", size, size))
		if err := renderIcon(source, out, size); err != nil {
			return fmt.Errorf("icon %dx%d: %w", size, size, err)
		}
	}
	// Also generate icon.png (512px) for general use
	if err := renderIcon(source, filepath.Join(outputDir, "icon.png"), 512); err != nil {
		return fmt.Errorf("icon.png: %w", err)
	}
	fmt.Println("  Generated standard icons")

	// Android mipmap icons
	androidRes := filepath.Join(projectDir, "frontend", "android", "app", "src", "main", "res")
	androidSizes := []struct {
		density string
		size    int
	}{
		{"mipmap-mdpi", 48},
		{"mipmap-hdpi", 72},
		{"mipmap-xhdpi", 96},
		{"mipmap-xxhdpi", 144},
		{"mipmap-xxxhdpi", 192},
	}
	for _, a := range androidSizes {
		dir := filepath.Join(androidRes, a.density)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		out := filepath.Join(dir, "ic_launcher.png")
		if err := renderIcon(source, out, a.size); err != nil {
			return fmt.Errorf("android %s: %w", a.density, err)
		}
	}
	fmt.Println("  Generated Android icons")

	// iOS app icons
	iosIconDir := filepath.Join(projectDir, "frontend", "ios", "Runner", "Assets.xcassets", "AppIcon.appiconset")
	if _, err := os.Stat(iosIconDir); err == nil {
		iosIcons := []struct {
			filename string
			size     int
		}{
			{"Icon-App-20x20@1x.png", 20},
			{"Icon-App-20x20@2x.png", 40},
			{"Icon-App-20x20@3x.png", 60},
			{"Icon-App-29x29@1x.png", 29},
			{"Icon-App-29x29@2x.png", 58},
			{"Icon-App-29x29@3x.png", 87},
			{"Icon-App-40x40@1x.png", 40},
			{"Icon-App-40x40@2x.png", 80},
			{"Icon-App-40x40@3x.png", 120},
			{"Icon-App-60x60@2x.png", 120},
			{"Icon-App-60x60@3x.png", 180},
			{"Icon-App-76x76@1x.png", 76},
			{"Icon-App-76x76@2x.png", 152},
			{"Icon-App-83.5x83.5@2x.png", 167},
			{"Icon-App-1024x1024@1x.png", 1024},
		}
		for _, i := range iosIcons {
			out := filepath.Join(iosIconDir, i.filename)
			if err := renderIcon(source, out, i.size); err != nil {
				return fmt.Errorf("ios %s: %w", i.filename, err)
			}
		}
		fmt.Println("  Generated iOS icons")
	}

	return nil
}

// findIconRenderer returns a function that renders an SVG to a PNG at the given size.
// Prefers rsvg-convert, falls back to ImageMagick.
func findIconRenderer() (func(source, output string, size int) error, error) {
	if rsvg, err := exec.LookPath("rsvg-convert"); err == nil {
		return func(source, output string, size int) error {
			cmd := exec.Command(rsvg, "-w", fmt.Sprintf("%d", size), "-h", fmt.Sprintf("%d", size), source, "-o", output)
			return cmd.Run()
		}, nil
	}

	if magick, err := exec.LookPath("convert"); err == nil {
		return func(source, output string, size int) error {
			cmd := exec.Command(magick, "-background", "none", "-resize", fmt.Sprintf("%dx%d", size, size), source, output)
			return cmd.Run()
		}, nil
	}

	return nil, fmt.Errorf("neither rsvg-convert nor ImageMagick found; install one to generate icons")
}

// --- doctor ---

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check environment for required tools",
	RunE: func(cmd *cobra.Command, args []string) error {
		checks := []struct {
			name     string
			command  string
			args     []string
			pathOnly bool // If true, just check the binary exists (skip version output)
		}{
			{"Go", "go", []string{"version"}, false},
			{"Flutter", "flutter", []string{"--version"}, false},
			{"GCC", "gcc", []string{"--version"}, false},
			{"pkg-config", "pkg-config", []string{"--version"}, false},
			{"rsvg-convert", "rsvg-convert", []string{"--version"}, false},
			{"flatpak-builder", "flatpak-builder", []string{"--version"}, false},
			{"appstreamcli", "appstreamcli", []string{"--version"}, false},
			{"appimagetool", "appimagetool", []string{"--version"}, false},
		}

		fmt.Print("\n🩺 Checking environment...\n\n")

		allOk := true
		for _, c := range checks {
			path, err := exec.LookPath(c.command)
			if err != nil {
				util.DoctorMissing(c.name, c.command)
				allOk = false
				continue
			}

			if c.pathOnly {
				util.DoctorOK(c.name, path)
				continue
			}

			out, err := exec.Command(path, c.args...).CombinedOutput()
			if err != nil {
				util.DoctorError(c.name, err)
				allOk = false
				continue
			}

			ver := strings.TrimSpace(strings.Split(string(out), "\n")[0])
			util.DoctorOK(c.name, ver)
		}

		if allOk {
			fmt.Println("\n✅ All tools found!")
		}
		if !allOk {
			fmt.Println("\n⚠️  Some tools are missing. Install them for full functionality.")
			fmt.Println("  Required: Go, Flutter, GCC")
			fmt.Println("  Optional: rsvg-convert, flatpak-builder, appstreamcli, appimagetool")
		}

		full, _ := cmd.Flags().GetBool("full")
		if full {
			flutterPath, err := exec.LookPath("flutter")
			if err != nil {
				return nil
			}
			fmt.Print("\n🦋 Flutter Doctor\n\n")
			fc := exec.Command(flutterPath, "doctor")
			fc.Stdout = os.Stdout
			fc.Stderr = os.Stderr
			// User reads doctor output directly; doctor's exit code is
			// informational and doesn't affect this command's success.
			_ = fc.Run()
		}

		return nil
	},
}

func init() {
	doctorCmd.Flags().Bool("full", false, "Also run flutter doctor")
}

// --- clean ---

// --- lint ---

var lintCmd = &cobra.Command{
	Use:   "lint [target]",
	Short: "Run Go (golangci-lint) and/or Dart (flutter analyze) linters on the project",
	Long: `Runs the requested linters and reports a combined exit status.

  flugo lint            both linters (default)
  flugo lint backend    Go only (golangci-lint run ./...)
  flugo lint frontend   Dart only (flutter analyze)

With --fix, also applies auto-fixes for the selected target(s):
  Go:   golangci-lint run --fix ./...   gofmt + import ordering + a few staticcheck fixes
  Dart: dart fix --apply                schema-driven Dart auto-fixes
        dart format .                   formatting

Pre-requisites (only what's needed for the chosen target):
  - golangci-lint   https://golangci-lint.run/welcome/install
  - flutter / dart  https://docs.flutter.dev/get-started/install`,
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: []string{"backend", "frontend"},
	RunE: func(cmd *cobra.Command, args []string) error {
		fix, _ := cmd.Flags().GetBool("fix")

		runBackend, runFrontend := true, true
		if len(args) == 1 {
			switch args[0] {
			case "backend":
				runFrontend = false
			case "frontend":
				runBackend = false
			default:
				return fmt.Errorf("unknown target %q (expected: backend, frontend, or no argument for both)", args[0])
			}
		}

		projectDir, err := findProject()
		if err != nil {
			return err
		}
		backendDir := filepath.Join(projectDir, "backend")
		frontendDir := filepath.Join(projectDir, "frontend")

		// Pre-flight: confirm tools are installed BEFORE doing anything, so the
		// user gets one clear error instead of partial work. Only check what
		// we're actually about to run.
		if runBackend {
			if _, err := exec.LookPath("golangci-lint"); err != nil {
				return fmt.Errorf("golangci-lint not found in PATH (install: https://golangci-lint.run/welcome/install)")
			}
		}
		if runFrontend {
			if _, err := exec.LookPath("flutter"); err != nil {
				return fmt.Errorf("flutter not found in PATH (install: https://docs.flutter.dev/get-started/install)")
			}
		}

		var failures []string

		// --- Go ---
		if runBackend {
			fmt.Println("🐹 Linting Go backend (golangci-lint)...")
			goArgs := []string{"run", "./..."}
			if fix {
				goArgs = []string{"run", "--fix", "./..."}
			}
			goLint := exec.Command("golangci-lint", goArgs...)
			goLint.Dir = backendDir
			goLint.Stdout = os.Stdout
			goLint.Stderr = os.Stderr
			if err := goLint.Run(); err != nil {
				failures = append(failures, fmt.Sprintf("Go lint failed: %v", err))
			}
		}

		// --- Dart auto-fix (only when --fix) ---
		if runFrontend && fix {
			fmt.Println("\n🦋 Auto-fixing Dart frontend (dart fix + format)...")
			dartFix := exec.Command("dart", "fix", "--apply")
			dartFix.Dir = frontendDir
			dartFix.Stdout = os.Stdout
			dartFix.Stderr = os.Stderr
			if err := dartFix.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠️  dart fix failed: %v\n", err)
			}
			dartFmt := exec.Command("dart", "format", ".")
			dartFmt.Dir = frontendDir
			dartFmt.Stdout = os.Stdout
			dartFmt.Stderr = os.Stderr
			if err := dartFmt.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠️  dart format failed: %v\n", err)
			}
		}

		// --- Dart analyze ---
		if runFrontend {
			fmt.Println("\n🦋 Linting Dart frontend (flutter analyze)...")
			flutterLint := exec.Command("flutter", "analyze")
			flutterLint.Dir = frontendDir
			flutterLint.Stdout = os.Stdout
			flutterLint.Stderr = os.Stderr
			if err := flutterLint.Run(); err != nil {
				failures = append(failures, fmt.Sprintf("Dart analyze failed: %v", err))
			}
		}

		if len(failures) > 0 {
			return fmt.Errorf("lint failures:\n  - %s", strings.Join(failures, "\n  - "))
		}
		fmt.Println("\n✅ All clean")
		return nil
	},
}

// --- clean ---

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean build artifacts",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, err := findProject()
		if err != nil {
			return err
		}

		// Run flutter clean first to clear .dart_tool caches
		frontendDir := filepath.Join(projectDir, "frontend")
		flutterClean := exec.Command("flutter", "clean")
		flutterClean.Dir = frontendDir
		flutterClean.Stdout = os.Stdout
		flutterClean.Stderr = os.Stderr
		if err := flutterClean.Run(); err != nil {
			fmt.Printf("  ⚠️  flutter clean failed: %v\n", err)
		}

		dirs := []string{
			filepath.Join(projectDir, "build"),
			filepath.Join(projectDir, "frontend", "build"),
		}

		for _, d := range dirs {
			if err := os.RemoveAll(d); err != nil {
				return fmt.Errorf("removing %s: %w", d, err)
			}
		}

		fmt.Println("🧹 Cleaned build artifacts")
		return nil
	},
}

// --- upgrade ---

var upgradeCmd = &cobra.Command{
	Use:   "upgrade [version/ref]",
	Short: "Self-update the flugo CLI binary",
	Long:  "Install a new version of the flugo CLI. Defaults to @latest.\nExamples: flugo upgrade, flugo upgrade v0.1.1, flugo upgrade main",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := exec.LookPath("go"); err != nil {
			return fmt.Errorf("go is required for flugo upgrade but was not found in PATH")
		}

		ref := "latest"
		if len(args) > 0 {
			ref = args[0]
		}
		// Reject a ref that could be parsed as an option by git checkout /
		// go install (leading '-'), so `flugo upgrade -- --foo` can't inject flags.
		if strings.HasPrefix(ref, "-") {
			return fmt.Errorf("invalid ref %q: must not start with '-'", ref)
		}

		flugoPath, _ := cmd.Flags().GetString("flugo-path")

		fmt.Printf("\n  Current version: %s\n", version)

		if flugoPath != "" {
			return upgradeFromLocal(flugoPath, ref)
		}
		return upgradeFromGitHub(ref)
	},
}

func init() {
	upgradeCmd.Flags().String("flugo-path", "", "Path to local flugo repo (build from source instead of go install)")
}

func upgradeFromGitHub(ref string) error {
	fmt.Printf("  Installing flugo@%s from GitHub...\n\n", ref)

	goCmd := exec.Command("go", "install", "github.com/hkdb/flugo/cmd/flugo@"+ref)
	var stderr bytes.Buffer
	goCmd.Stdout = os.Stdout
	goCmd.Stderr = &stderr

	if err := goCmd.Run(); err != nil {
		errMsg := stderr.String()
		if ref == "latest" && (strings.Contains(errMsg, "no matching versions") || strings.Contains(errMsg, "no versions")) {
			fmt.Println("  Already the latest release. Nothing to upgrade...")
			return nil
		}
		return fmt.Errorf("go install failed: %w\n%s", err, errMsg)
	}

	fmt.Println("  ✅ Upgrade complete!")
	return nil
}

func upgradeFromLocal(repoPath string, ref string) error {
	fmt.Printf("  Building flugo@%s from local repo: %s\n\n", ref, repoPath)

	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git is required for --flugo-path but was not found in PATH")
	}

	// Resolve the current binary path.
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving current binary path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("resolving symlinks for binary: %w", err)
	}

	// Track current branch.
	branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	branchCmd.Dir = repoPath
	branchOut, err := branchCmd.Output()
	if err != nil {
		return fmt.Errorf("getting current branch: %w", err)
	}
	originalBranch := strings.TrimSpace(string(branchOut))

	// Stash uncommitted changes.
	stashCmd := exec.Command("git", "stash")
	stashCmd.Dir = repoPath
	stashOut, err := stashCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git stash: %w\n%s", err, stashOut)
	}
	stashCreated := !strings.Contains(string(stashOut), "No local changes")

	// Restore original state on exit. Best-effort: if either step fails the
	// user is left on a different branch / with an unpopped stash. We log so
	// they can recover manually rather than silently leaving them confused.
	restore := func() {
		checkoutCmd := exec.Command("git", "checkout", originalBranch)
		checkoutCmd.Dir = repoPath
		if out, err := checkoutCmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to restore branch %q: %v\n%s\n", originalBranch, err, out)
		}

		if stashCreated {
			popCmd := exec.Command("git", "stash", "pop")
			popCmd.Dir = repoPath
			if out, err := popCmd.CombinedOutput(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to pop git stash: %v\n%s\nyou may need to run 'git stash pop' manually\n", err, out)
			}
		}
	}

	// Checkout the requested ref.
	coCmd := exec.Command("git", "checkout", ref)
	coCmd.Dir = repoPath
	if out, err := coCmd.CombinedOutput(); err != nil {
		restore()
		return fmt.Errorf("git checkout %s: %w\n%s", ref, err, out)
	}

	// Build to a temp file.
	tmpFile, err := os.CreateTemp("", "flugo-upgrade-*")
	if err != nil {
		restore()
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	buildCmd := exec.Command("go", "build", "-o", tmpPath, "./cmd/flugo")
	buildCmd.Dir = repoPath
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr

	if err := buildCmd.Run(); err != nil {
		os.Remove(tmpPath)
		restore()
		return fmt.Errorf("go build failed: %w", err)
	}

	// Restore original branch before replacing binary.
	restore()

	// Replace the current binary.
	if err := os.Rename(tmpPath, exePath); err != nil {
		// Rename may fail across filesystems; fall back to copy.
		srcData, readErr := os.ReadFile(tmpPath)
		os.Remove(tmpPath)
		if readErr != nil {
			return fmt.Errorf("reading built binary: %w", readErr)
		}
		if err := os.WriteFile(exePath, srcData, 0o755); err != nil {
			return fmt.Errorf("replacing binary: %w", err)
		}
	}

	fmt.Println("  ✅ Upgrade complete!")
	return nil
}

// --- version ---

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print Flugo version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("flugo %s\n", version)
	},
}

// --- helpers ---

func findProject() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return config.FindProjectRoot(cwd)
}
