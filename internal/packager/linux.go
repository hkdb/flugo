package packager

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

//go:embed assets/linux-install.sh
var linuxInstallScript []byte

//go:embed assets/linux-uninstall.sh
var linuxUninstallScript []byte

// packageLinux builds an installable tarball: the Flutter bundle under app/,
// plus install.sh/uninstall.sh (+ app.env), the .desktop entry, and icons — so
// a user can extract it and run ./install.sh for full desktop integration.
func (p *Packager) packageLinux() error {
	fmt.Println("Packaging for Linux...")

	// Flutter emits the desktop bundle under an arch-specific dir; the tarball
	// name carries the conventional arch label.
	flutterArch, tarArch := "x64", "x86_64"
	if runtime.GOARCH == "arm64" {
		flutterArch, tarArch = "arm64", "aarch64"
	}

	bundleDir := filepath.Join(p.projectDir, "frontend", "build", "linux", flutterArch, "release", "bundle")
	if _, err := os.Stat(bundleDir); err != nil {
		return fmt.Errorf("build not found at %s; run 'flugo build linux' first", bundleDir)
	}

	outputDir := filepath.Join(p.projectDir, "build", "package", "linux")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}

	appName := p.cfg.App.Name
	appID := p.cfg.App.ID
	version := p.cfg.App.Version
	binName := binaryName(appID)
	slug := slugify(appName)

	// Assemble a staging tree: <slug>/{install.sh,uninstall.sh,app.env,<id>.desktop,icons/,app/}
	stageRoot := filepath.Join(outputDir, "stage")
	if err := os.RemoveAll(stageRoot); err != nil {
		return err
	}
	top := filepath.Join(stageRoot, slug)
	appDir := filepath.Join(top, "app")
	iconsDir := filepath.Join(top, "icons")
	for _, d := range []string{appDir, iconsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	// App payload — cp -a preserves executable bits and symlinks.
	if err := run("cp", "-a", bundleDir+"/.", appDir); err != nil {
		return fmt.Errorf("copying bundle: %w", err)
	}

	// Installer scripts + the per-app values they source.
	if err := os.WriteFile(filepath.Join(top, "install.sh"), linuxInstallScript, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(top, "uninstall.sh"), linuxUninstallScript, 0o755); err != nil {
		return err
	}
	appEnv := fmt.Sprintf("APP_NAME=%q\nAPP_ID=%q\nBIN_NAME=%q\nSLUG=%q\n", appName, appID, binName, slug)
	if err := os.WriteFile(filepath.Join(top, "app.env"), []byte(appEnv), 0o644); err != nil {
		return err
	}

	if err := p.stageDesktop(top, appID, binName); err != nil {
		return err
	}
	if err := p.stageIcons(iconsDir); err != nil {
		return err
	}

	// Tarball with a friendly top-level <slug>/ directory (not "bundle").
	tarName := fmt.Sprintf("%s-%s-linux-%s.tar.gz", appName, version, tarArch)
	tarPath := filepath.Join(outputDir, tarName)
	if err := run("tar", "-czf", tarPath, "-C", stageRoot, slug); err != nil {
		return fmt.Errorf("creating tarball: %w", err)
	}
	_ = os.RemoveAll(stageRoot)

	fmt.Printf("Package created: %s\n", tarPath)
	return nil
}

// stageDesktop copies the project's assets/linux/<appid>.desktop into the
// tarball if present, otherwise generates a minimal default from the config.
func (p *Packager) stageDesktop(top, appID, binName string) error {
	dst := filepath.Join(top, appID+".desktop")

	src := filepath.Join(p.projectDir, "assets", "linux", appID+".desktop")
	if data, err := os.ReadFile(src); err == nil {
		return os.WriteFile(dst, data, 0o644)
	}

	execLine := binName
	mime := ""
	if p.cfg.App.URLScheme != "" {
		execLine = binName + " %u"
		mime = fmt.Sprintf("MimeType=x-scheme-handler/%s;\n", p.cfg.App.URLScheme)
	}
	content := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=%s
Comment=%s
Exec=%s
Icon=%s
Terminal=false
Categories=Utility;
%s`, p.cfg.App.Name, p.cfg.App.Name, execLine, appID, mime)
	return os.WriteFile(dst, []byte(content), 0o644)
}

// stageIcons copies the app's PNG icons (icon-<size>x<size>.png) and an SVG into
// the tarball's icons/ dir; install.sh maps them into the hicolor theme.
func (p *Packager) stageIcons(iconsDir string) error {
	iconSrcDir := filepath.Join(p.projectDir, "assets", "icons")

	pngs, _ := filepath.Glob(filepath.Join(iconSrcDir, "icon-*x*.png"))
	for _, png := range pngs {
		if err := copyFileMode(png, filepath.Join(iconsDir, filepath.Base(png)), 0o644); err != nil {
			return err
		}
	}

	// Prefer assets/icons/icon.svg, else the configured source SVG.
	svg := filepath.Join(iconSrcDir, "icon.svg")
	if _, err := os.Stat(svg); err != nil && p.cfg.Icons.Source != "" {
		svg = filepath.Join(p.projectDir, p.cfg.Icons.Source)
	}
	if _, err := os.Stat(svg); err == nil {
		if err := copyFileMode(svg, filepath.Join(iconsDir, "icon.svg"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// run executes a command, forwarding stdout/stderr.
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// binaryName mirrors the Flutter BINARY_NAME: the app ID's last dot-segment with
// hyphens converted to underscores (e.g. io.github.x.ic-app → ic_app).
func binaryName(appID string) string {
	seg := appID
	if i := strings.LastIndex(seg, "."); i >= 0 {
		seg = seg[i+1:]
	}
	return strings.ReplaceAll(seg, "-", "_")
}

// slugify lowercases and dash-joins the app name, keeping only [a-z0-9-], for
// use as the tarball's top dir, the install prefix, and the launcher name.
func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "-")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "app"
	}
	return b.String()
}

// copyFileMode copies src to dst with the given mode.
func copyFileMode(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode)
}
