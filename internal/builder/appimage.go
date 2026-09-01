package builder

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// buildAppImage assembles an AppDir and runs appimagetool to produce an AppImage.
func (b *Builder) buildAppImage() error {
	fmt.Println("\n📦 Building AppImage...")

	if _, err := exec.LookPath("appimagetool"); err != nil {
		fmt.Println("  ⚠️  appimagetool not found in PATH — attempting to install...")
		if installErr := installAppImageTool(); installErr != nil {
			return fmt.Errorf("appimagetool not found and auto-install failed: %w", installErr)
		}
	}

	appimageDir := filepath.Join(b.projectDir, "assets", "linux", "appimage")
	appRunPath := filepath.Join(appimageDir, "AppRun")
	if _, err := stat(appRunPath); err != nil {
		return fmt.Errorf("AppImage assets not found at %s — run 'flugo appimage' first", appimageDir)
	}

	// Derive short name from app ID.
	name := b.cfg.App.ID
	if parts := strings.Split(name, "."); len(parts) > 0 {
		name = parts[len(parts)-1]
	}

	// Auto-build linux release if not already built.
	bundleDir := filepath.Join(b.frontendDir(), "build", "linux", "x64", "release", "bundle")
	if _, err := stat(bundleDir); err != nil {
		fmt.Println("  ⚙️  Release build not found, building linux first...")
		if err := b.buildDesktop("linux", true); err != nil {
			return fmt.Errorf("auto-building linux: %w", err)
		}
	}

	// Assemble AppDir.
	appDir := filepath.Join(b.buildDir(), "appimage", "AppDir")
	if err := os.RemoveAll(appDir); err != nil {
		return fmt.Errorf("cleaning AppDir: %w", err)
	}

	usrBin := filepath.Join(appDir, "usr", "bin")
	usrLib := filepath.Join(appDir, "usr", "lib")
	if err := os.MkdirAll(usrBin, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(usrLib, 0o755); err != nil {
		return err
	}

	// Copy AppRun (ensure executable).
	if err := copyFile(appRunPath, filepath.Join(appDir, "AppRun"), 0o755); err != nil {
		return fmt.Errorf("copying AppRun: %w", err)
	}

	// Copy .desktop file — use full app ID to match Icon= field.
	appID := b.cfg.App.ID
	desktopSrc := filepath.Join(appimageDir, appID+".desktop")
	if err := copyFile(desktopSrc, filepath.Join(appDir, appID+".desktop"), 0); err != nil {
		return fmt.Errorf("copying .desktop: %w", err)
	}

	// Copy icon — filename must match Icon= value in .desktop file.
	iconSrc := filepath.Join(appimageDir, name+".png")
	if err := copyFile(iconSrc, filepath.Join(appDir, appID+".png"), 0); err != nil {
		return fmt.Errorf("copying icon: %w", err)
	}

	// Copy Flutter bundle contents into usr/bin/.
	if err := copyDir(bundleDir, usrBin); err != nil {
		return fmt.Errorf("copying Flutter bundle: %w", err)
	}

	// Copy libbackend.so into usr/lib/.
	libSrc := filepath.Join(b.buildDir(), "linux", "libbackend.so")
	if err := copyFile(libSrc, filepath.Join(usrLib, "libbackend.so"), 0); err != nil {
		return fmt.Errorf("copying libbackend.so: %w", err)
	}

	// Bundle shared library dependencies.
	dartName := strings.ReplaceAll(name, "-", "_")
	binaries := []string{
		filepath.Join(usrBin, dartName),
		filepath.Join(usrLib, "libbackend.so"),
	}
	// Also include any .so files from the Flutter bundle's lib/ directory.
	bundleLibDir := filepath.Join(usrBin, "lib")
	if entries, err := os.ReadDir(bundleLibDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".so") {
				binaries = append(binaries, filepath.Join(bundleLibDir, e.Name()))
			}
		}
	}
	fmt.Println("  ⚙️  Bundling shared library dependencies...")
	bundleDeps(binaries, usrLib)

	// Run appimagetool.
	packageDir := filepath.Join(b.buildDir(), "package", "linux")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		return err
	}

	outputName := fmt.Sprintf("%s-%s-x86_64.AppImage", b.cfg.App.Name, b.cfg.App.Version)
	outputPath := filepath.Join(packageDir, outputName)

	fmt.Println("  ⚙️  Running appimagetool...")
	args := []string{appDir, outputPath}
	if err := runCommand("appimagetool", args, b.projectDir, nil); err != nil {
		return fmt.Errorf("appimagetool failed: %w", err)
	}

	fmt.Printf("  ✅ AppImage built: build/package/linux/%s\n", outputName)
	return nil
}

// stat is a convenience wrapper around os.Stat.
func stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

// copyFile copies a single file. If perm is 0, the source file's permissions are preserved.
func copyFile(src, dst string, perm fs.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	if perm == 0 {
		info, err := os.Stat(src)
		if err != nil {
			return err
		}
		perm = info.Mode()
	}

	return os.WriteFile(dst, data, perm)
}

// copyDir recursively copies the contents of src into dst.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		return copyFile(path, target, 0)
	})
}

// systemLibs lists library prefixes that should NOT be bundled — they must come
// from the host system to avoid ABI mismatches or segfaults.
var systemLibs = []string{
	"libc.so",
	"libm.so",
	"libdl.so",
	"librt.so",
	"libpthread.so",
	"libutil.so",
	"libnsl.so",
	"libcrypt.so",
	"libresolv.so",
	"ld-linux",
	"linux-vdso",
	"libGL.so",
	"libEGL.so",
	"libGLX.so",
	"libGLdispatch.so",
	"libvulkan.so",
	"libdrm.so",
	"libnvidia",
	"libX11.so",
	"libxcb.so",
	"libwayland",
}

// isSystemLib returns true if the given library name matches a system lib prefix.
func isSystemLib(name string) bool {
	for _, prefix := range systemLibs {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// lddParse runs ldd on the given binary and returns a map of lib name → absolute path
// for all resolved shared libraries.
func lddParse(binary string) (map[string]string, error) {
	out, err := exec.Command("ldd", binary).Output()
	if err != nil {
		return nil, fmt.Errorf("ldd %s: %w", binary, err)
	}

	libs := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Typical ldd output:  libfoo.so.1 => /usr/lib/libfoo.so.1 (0x...)
		parts := strings.SplitN(line, " => ", 2)
		if len(parts) != 2 {
			continue
		}
		libName := strings.TrimSpace(parts[0])
		rest := strings.TrimSpace(parts[1])

		// Strip the (0x...) address suffix.
		if idx := strings.LastIndex(rest, " (0x"); idx > 0 {
			rest = strings.TrimSpace(rest[:idx])
		}
		if rest == "" || rest == "not found" {
			continue
		}
		libs[libName] = rest
	}
	return libs, nil
}

// bundleDeps traces shared library dependencies for the given binaries and copies
// non-system libs into destLib. Best-effort: per-library failures are logged
// and skipped (an AppImage with a few missing deps still runs in many cases),
// so this function never returns an error.
func bundleDeps(binaries []string, destLib string) {
	allLibs := make(map[string]string)
	for _, bin := range binaries {
		if _, err := os.Stat(bin); err != nil {
			continue
		}
		libs, err := lddParse(bin)
		if err != nil {
			fmt.Printf("    ⚠️  skipping ldd for %s: %v\n", filepath.Base(bin), err)
			continue
		}
		for name, path := range libs {
			allLibs[name] = path
		}
	}

	bundled := 0
	for name, srcPath := range allLibs {
		if isSystemLib(name) {
			continue
		}
		destPath := filepath.Join(destLib, filepath.Base(srcPath))
		if _, err := os.Stat(destPath); err == nil {
			continue // Already in AppDir (e.g. from Flutter bundle).
		}
		if err := copyFile(srcPath, destPath, 0); err != nil {
			fmt.Printf("    ⚠️  could not bundle %s: %v\n", name, err)
			continue
		}
		bundled++
	}
	fmt.Printf("    ✅ Bundled %d shared libraries\n", bundled)
}

// installAppImageTool downloads appimagetool from GitHub and installs it to ~/.local/bin/.
// Pinned appimagetool release and per-arch SHA-256 of its AppImage assets.
// appimagetool is downloaded, verified against these digests, and only then made
// executable and run — an unverified binary is a supply-chain RCE. Bump the
// version and BOTH checksums together (they're from the release assets;
// TestAppImageToolSHA256MatchesUpstream guards them when run online).
const appImageToolVersion = "1.9.1"

var appImageToolSHA256 = map[string]string{ // key: mapped arch (x86_64/aarch64)
	"x86_64":  "ed4ce84f0d9caff66f50bcca6ff6f35aae54ce8135408b3fa33abfc3cb384eb0",
	"aarch64": "f0837e7448a0c1e4e650a93bb3e85802546e60654ef287576f46c71c126a9158",
}

func installAppImageTool() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("AppImage is only supported on Linux")
	}

	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	default:
		return fmt.Errorf("unsupported architecture for appimagetool: %s", arch)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolving home directory: %w", err)
	}

	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", binDir, err)
	}

	wantSum, ok := appImageToolSHA256[arch]
	if !ok {
		return fmt.Errorf("no pinned appimagetool checksum for arch %s", arch)
	}
	url := fmt.Sprintf("https://github.com/AppImage/appimagetool/releases/download/%s/appimagetool-%s.AppImage", appImageToolVersion, arch)
	destPath := filepath.Join(binDir, "appimagetool")

	fmt.Printf("  ⬇️  Downloading appimagetool %s...\n", appImageToolVersion)

	// Download to a temp file and verify the SHA-256 BEFORE making it executable
	// or moving it into place — never chmod +x / run an unverified download.
	tmp, err := os.CreateTemp(binDir, ".appimagetool-*.download")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op after a successful rename

	resp, err := http.Get(url)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("downloading appimagetool: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		tmp.Close()
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("writing appimagetool: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}

	if got := hex.EncodeToString(h.Sum(nil)); got != wantSum {
		return fmt.Errorf("appimagetool checksum mismatch for %s: got %s, want %s (refusing to install)", arch, got, wantSum)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("chmod appimagetool: %w", err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("installing appimagetool: %w", err)
	}

	// Verify it's now reachable. If ~/.local/bin isn't in PATH, add it temporarily.
	if _, err := exec.LookPath("appimagetool"); err != nil {
		os.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	}

	fmt.Printf("  ✅ Installed appimagetool to %s\n", destPath)
	return nil
}
