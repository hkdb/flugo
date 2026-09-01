//go:build windows

package bridge

import (
	"fmt"
	"os"

	winreg "golang.org/x/sys/windows/registry"
)

// RegisterURLScheme registers this executable as the handler for
// scheme:// links in the per-user registry (no admin rights needed).
// Idempotent — call it unconditionally at startup; it rewrites the keys so
// a moved executable re-registers itself. No-op on other platforms (their
// registration is static: Info.plist, AndroidManifest, .desktop).
func RegisterURLScheme(scheme string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving executable: %w", err)
	}
	base := `Software\Classes\` + scheme

	root, _, err := winreg.CreateKey(winreg.CURRENT_USER, base, winreg.SET_VALUE)
	if err != nil {
		return fmt.Errorf("creating scheme key: %w", err)
	}
	defer root.Close()
	if err := root.SetStringValue("", "URL:"+scheme); err != nil {
		return err
	}
	if err := root.SetStringValue("URL Protocol", ""); err != nil {
		return err
	}

	cmd, _, err := winreg.CreateKey(winreg.CURRENT_USER, base+`\shell\open\command`, winreg.SET_VALUE)
	if err != nil {
		return fmt.Errorf("creating command key: %w", err)
	}
	defer cmd.Close()
	return cmd.SetStringValue("", fmt.Sprintf(`"%s" "%%1"`, exe))
}
