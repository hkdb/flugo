package packager

import (
	"fmt"
	"os"
	"path/filepath"
)

// packageAndroid points to the Flutter Android build output.
func (p *Packager) packageAndroid() error {
	fmt.Println("Packaging for Android...")

	apkPath := filepath.Join(p.projectDir, "frontend", "build", "app", "outputs", "flutter-apk", "app-release.apk")
	aabPath := filepath.Join(p.projectDir, "frontend", "build", "app", "outputs", "bundle", "release", "app-release.aab")

	if _, err := os.Stat(apkPath); err == nil {
		fmt.Printf("APK: %s\n", apkPath)
	}
	if _, err := os.Stat(aabPath); err == nil {
		fmt.Printf("AAB: %s\n", aabPath)
	}

	if _, err := os.Stat(apkPath); err != nil {
		if _, err := os.Stat(aabPath); err != nil {
			return fmt.Errorf("no Android build found; run 'flugo build android' first")
		}
	}

	return nil
}
