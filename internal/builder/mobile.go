package builder

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// androidABI describes a target Android architecture.
type androidABI struct {
	abi      string // e.g. "arm64-v8a"
	goarch   string // e.g. "arm64"
	ccPrefix string // e.g. "aarch64-linux-android"
}

var androidABIs = []androidABI{
	{"arm64-v8a", "arm64", "aarch64-linux-android"},
	{"armeabi-v7a", "arm", "armv7a-linux-androideabi"},
	{"x86_64", "amd64", "x86_64-linux-android"},
	{"x86", "386", "i686-linux-android"},
}

// buildAndroid cross-compiles the Go backend as a C shared library for each
// Android ABI using the NDK toolchain, then builds the Flutter APK.
func (b *Builder) buildAndroid(release bool) error {
	sdkPath, err := findAndroidSDK()
	if err != nil {
		return err
	}

	ndkPath, err := findNDK(sdkPath)
	if err != nil {
		return err
	}

	host := ndkHostPlatform()
	minSDK := b.cfg.Platforms.Android.MinSDK
	if minSDK == 0 {
		minSDK = 21
	}

	for _, abi := range androidABIs {
		fmt.Printf("  Building Go backend (%s)...\n", abi.abi)

		outDir := filepath.Join(b.frontendDir(), "android", "app", "src", "main", "jniLibs", abi.abi)
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return fmt.Errorf("creating jniLibs/%s: %w", abi.abi, err)
		}

		cc := filepath.Join(ndkPath, "toolchains", "llvm", "prebuilt", host, "bin",
			fmt.Sprintf("%s%d-clang", abi.ccPrefix, minSDK))

		env := []string{
			"CGO_ENABLED=1",
			"GOOS=android",
			"GOARCH=" + abi.goarch,
			"CC=" + cc,
		}

		args := []string{
			"build", "-buildmode=c-shared",
			"-o", filepath.Join(outDir, "libbackend.so"),
			".",
		}

		if err := runCommand("go", args, b.backendDir(), env); err != nil {
			return fmt.Errorf("go build (%s): %w", abi.abi, err)
		}
	}

	flutterArgs := []string{"build", "apk"}
	if release {
		flutterArgs = append(flutterArgs, "--release")
	}

	fmt.Println("  Building Flutter app (android)...")
	return runCommand("flutter", flutterArgs, b.frontendDir(), nil)
}

// buildIOS cross-compiles the Go backend as a C archive for iOS,
// then builds the Flutter app.
func (b *Builder) buildIOS(release bool) error {
	fmt.Println("  Building Go backend for iOS...")

	// Find iOS SDK via xcrun
	sdkPath, err := exec.Command("xcrun", "--sdk", "iphoneos", "--show-sdk-path").Output()
	if err != nil {
		return fmt.Errorf("finding iOS SDK (is Xcode installed?): %w", err)
	}
	sdk := strings.TrimSpace(string(sdkPath))

	// Find clang for iOS
	ccPath, err := exec.Command("xcrun", "--sdk", "iphoneos", "--find", "clang").Output()
	if err != nil {
		return fmt.Errorf("finding iOS clang: %w", err)
	}
	cc := strings.TrimSpace(string(ccPath))

	outDir := filepath.Join(b.frontendDir(), "ios", "Runner")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating iOS output dir: %w", err)
	}

	outputPath := filepath.Join(outDir, "libbackend.a")

	env := []string{
		"CGO_ENABLED=1",
		"GOOS=ios",
		"GOARCH=arm64",
		fmt.Sprintf("CC=%s", cc),
		fmt.Sprintf("CGO_CFLAGS=-isysroot %s -arch arm64 -miphoneos-version-min=%s", sdk, b.cfg.Platforms.IOS.MinimumVersion),
		fmt.Sprintf("CGO_LDFLAGS=-isysroot %s -arch arm64 -miphoneos-version-min=%s", sdk, b.cfg.Platforms.IOS.MinimumVersion),
	}

	args := []string{
		"build", "-buildmode=c-archive",
		"-o", outputPath,
		".",
	}

	if err := runCommand("go", args, b.backendDir(), env); err != nil {
		return fmt.Errorf("go build (ios): %w", err)
	}

	flutterArgs := []string{"build", "ios"}
	if release {
		flutterArgs = append(flutterArgs, "--release")
	}

	fmt.Println("  Building Flutter app (ios)...")
	return runCommand("flutter", flutterArgs, b.frontendDir(), nil)
}

// runAndroid builds the Go backend for the connected device's architecture
// and launches the app with flutter run.
func (b *Builder) runAndroid() error {
	sdkPath, err := findAndroidSDK()
	if err != nil {
		return err
	}

	ndkPath, err := findNDK(sdkPath)
	if err != nil {
		return err
	}

	host := ndkHostPlatform()
	minSDK := b.cfg.Platforms.Android.MinSDK
	if minSDK == 0 {
		minSDK = 21
	}

	// Detect connected device architecture
	deviceABI := detectAndroidABI()

	fmt.Printf("  Building Go backend (%s)...\n", deviceABI.abi)

	outDir := filepath.Join(b.frontendDir(), "android", "app", "src", "main", "jniLibs", deviceABI.abi)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating jniLibs/%s: %w", deviceABI.abi, err)
	}

	cc := filepath.Join(ndkPath, "toolchains", "llvm", "prebuilt", host, "bin",
		fmt.Sprintf("%s%d-clang", deviceABI.ccPrefix, minSDK))

	env := []string{
		"CGO_ENABLED=1",
		"GOOS=android",
		"GOARCH=" + deviceABI.goarch,
		"CC=" + cc,
	}

	args := []string{
		"build", "-buildmode=c-shared",
		"-o", filepath.Join(outDir, "libbackend.so"),
		".",
	}

	if err := runCommand("go", args, b.backendDir(), env); err != nil {
		return fmt.Errorf("go build (%s): %w", deviceABI.abi, err)
	}

	fmt.Println("  🚀 Running Flutter app (android)...")
	return runCommand("flutter", []string{"run", "-d", "android"}, b.frontendDir(), nil)
}

// runIOS builds the Go backend for iOS and launches with flutter run.
func (b *Builder) runIOS() error {
	fmt.Println("  Building Go backend for iOS...")

	sdkPath, err := exec.Command("xcrun", "--sdk", "iphoneos", "--show-sdk-path").Output()
	if err != nil {
		return fmt.Errorf("finding iOS SDK (is Xcode installed?): %w", err)
	}
	sdk := strings.TrimSpace(string(sdkPath))

	ccPath, err := exec.Command("xcrun", "--sdk", "iphoneos", "--find", "clang").Output()
	if err != nil {
		return fmt.Errorf("finding iOS clang: %w", err)
	}
	cc := strings.TrimSpace(string(ccPath))

	minVersion := b.cfg.Platforms.IOS.MinimumVersion
	if minVersion == "" {
		minVersion = "16.0"
	}

	outDir := filepath.Join(b.frontendDir(), "ios", "Runner")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating iOS output dir: %w", err)
	}

	outputPath := filepath.Join(outDir, "libbackend.a")

	env := []string{
		"CGO_ENABLED=1",
		"GOOS=ios",
		"GOARCH=arm64",
		fmt.Sprintf("CC=%s", cc),
		fmt.Sprintf("CGO_CFLAGS=-isysroot %s -arch arm64 -miphoneos-version-min=%s", sdk, minVersion),
		fmt.Sprintf("CGO_LDFLAGS=-isysroot %s -arch arm64 -miphoneos-version-min=%s", sdk, minVersion),
	}

	args := []string{
		"build", "-buildmode=c-archive",
		"-o", outputPath,
		".",
	}

	if err := runCommand("go", args, b.backendDir(), env); err != nil {
		return fmt.Errorf("go build (ios): %w", err)
	}

	fmt.Println("  🚀 Running Flutter app (ios)...")
	return runCommand("flutter", []string{"run", "-d", "ios"}, b.frontendDir(), nil)
}

// detectAndroidABI detects the connected Android device's ABI via adb.
// Falls back to arm64-v8a if detection fails.
func detectAndroidABI() androidABI {
	out, err := exec.Command("adb", "shell", "getprop", "ro.product.cpu.abi").Output()
	if err != nil {
		fmt.Println("  ⚠️  Could not detect device ABI, defaulting to arm64-v8a")
		return androidABIs[0] // arm64-v8a
	}
	detected := strings.TrimSpace(string(out))
	for _, abi := range androidABIs {
		if abi.abi == detected {
			return abi
		}
	}
	fmt.Printf("  ⚠️  Unknown ABI %q, defaulting to arm64-v8a\n", detected)
	return androidABIs[0]
}

// findAndroidSDK locates the Android SDK directory.
func findAndroidSDK() (string, error) {
	// Check environment variables first
	for _, env := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if val := os.Getenv(env); val != "" {
			if _, err := os.Stat(val); err == nil {
				return val, nil
			}
		}
	}

	// Ask Flutter — it knows where the SDK is regardless of install method
	path, err := flutterAndroidSDKPath()
	if err == nil && path != "" {
		return path, nil
	}

	return "", fmt.Errorf("android SDK not found; set ANDROID_HOME or configure it in Flutter (flutter config --android-sdk <path>)")
}

// flutterAndroidSDKPath parses `flutter config --list` to find the android-sdk path.
func flutterAndroidSDKPath() (string, error) {
	cmd := exec.Command("flutter", "config", "--list")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "android-sdk:") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}
		val := strings.TrimSpace(parts[1])
		if val == "" || val == "(Not set)" {
			continue
		}
		if _, err := os.Stat(val); err == nil {
			return val, nil
		}
	}
	return "", fmt.Errorf("android-sdk not set in flutter config")
}

// findNDK locates the Android NDK within the SDK directory.
// If not found, offers to install it via sdkmanager.
func findNDK(sdkPath string) (string, error) {
	// Check environment variable
	if val := os.Getenv("ANDROID_NDK_HOME"); val != "" {
		if _, err := os.Stat(val); err == nil {
			return val, nil
		}
	}

	// Check <sdk>/ndk/ — pick latest version
	ndkParent := filepath.Join(sdkPath, "ndk")
	entries, err := os.ReadDir(ndkParent)
	if err == nil && len(entries) > 0 {
		versions := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			versions = append(versions, e.Name())
		}
		if len(versions) > 0 {
			sort.Strings(versions)
			return filepath.Join(ndkParent, versions[len(versions)-1]), nil
		}
	}

	// Check legacy ndk-bundle location
	ndkBundle := filepath.Join(sdkPath, "ndk-bundle")
	if _, err := os.Stat(ndkBundle); err == nil {
		return ndkBundle, nil
	}

	// Offer to install
	fmt.Print("  Android NDK not found. Install it now? [y/N]: ")
	var answer string
	_, _ = fmt.Scanln(&answer)
	if strings.ToLower(strings.TrimSpace(answer)) != "y" {
		return "", fmt.Errorf("android NDK is required for Android builds. Install with: sdkmanager \"ndk;<version>\"")
	}

	sdkmanager := findSdkmanager(sdkPath)
	if sdkmanager == "" {
		return "", fmt.Errorf("sdkmanager not found — install NDK manually: sdkmanager \"ndk;<version>\"")
	}

	// Find latest NDK version available
	ndkPkg, err := findLatestNDKPackage(sdkmanager)
	if err != nil {
		return "", fmt.Errorf("could not determine latest NDK version: %w", err)
	}

	fmt.Printf("  Installing %s...\n", ndkPkg)
	cmd := exec.Command(sdkmanager, ndkPkg)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("sdkmanager install failed: %w", err)
	}

	// Try again after install
	entries, err = os.ReadDir(ndkParent)
	if err != nil {
		return "", fmt.Errorf("NDK install succeeded but directory not found")
	}
	versions := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		versions = append(versions, e.Name())
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("NDK install succeeded but no version directory found")
	}
	sort.Strings(versions)
	return filepath.Join(ndkParent, versions[len(versions)-1]), nil
}

// findSdkmanager locates the sdkmanager binary within the SDK.
func findSdkmanager(sdkPath string) string {
	candidates := []string{
		filepath.Join(sdkPath, "cmdline-tools", "latest", "bin", "sdkmanager"),
		filepath.Join(sdkPath, "tools", "bin", "sdkmanager"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	// Try PATH
	if path, err := exec.LookPath("sdkmanager"); err == nil {
		return path
	}
	return ""
}

// findLatestNDKPackage queries sdkmanager --list to find the latest NDK package name.
func findLatestNDKPackage(sdkmanager string) (string, error) {
	cmd := exec.Command(sdkmanager, "--list")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	var latest string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "ndk;") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		latest = fields[0] // e.g. "ndk;29.0.14206865"
	}

	if latest == "" {
		return "", fmt.Errorf("no NDK packages found in sdkmanager")
	}
	return latest, nil
}

// ndkHostPlatform returns the host platform string for NDK toolchain paths.
func ndkHostPlatform() string {
	switch runtime.GOOS {
	case "darwin":
		return "darwin-x86_64"
	case "windows":
		return "windows-x86_64"
	default:
		return "linux-x86_64"
	}
}
