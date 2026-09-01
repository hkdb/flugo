package packager

import (
	"fmt"
	"os"
	"path/filepath"
)

// packageIOS points to the Flutter iOS build output.
func (p *Packager) packageIOS() error {
	fmt.Println("Packaging for iOS...")

	buildDir := filepath.Join(p.projectDir, "frontend", "build", "ios", "ipa")
	if _, err := os.Stat(buildDir); err != nil {
		return fmt.Errorf("no iOS build found; run 'flugo build ios' first")
	}

	fmt.Printf("iOS IPA: %s\n", buildDir)
	return nil
}
