// Package packager creates distributable packages for each platform.
package packager

import (
	"fmt"

	"github.com/hkdb/flugo/internal/config"
)

// Packager creates distributable packages.
type Packager struct {
	projectDir string
	cfg        *config.Config
}

// New creates a Packager.
func New(projectDir string, cfg *config.Config) *Packager {
	return &Packager{
		projectDir: projectDir,
		cfg:        cfg,
	}
}

// Package creates a distributable for the given platform.
func (p *Packager) Package(platform string) error {
	switch platform {
	case "linux":
		return p.packageLinux()
	case "macos":
		return p.packageMacOS()
	case "windows":
		return p.packageWindows()
	case "android":
		return p.packageAndroid()
	case "ios":
		return p.packageIOS()
	default:
		return fmt.Errorf("unknown platform: %s", platform)
	}
}
