// Package appinfo is the application's identity as flugo stamps it into the
// Go backend at build time: `flugo build` and `flugo run` pass the name, id
// and version from flugo.yaml via `-ldflags -X`, so a backend can log or
// report the version it was built as with no per-app wiring. A backend built
// with a plain `go build` reports Version "dev" and empty name/id. The same
// values reach Dart through the built-in flugoAppInfoService bridge service
// (`FlugoBridge.callAsync('flugoAppInfoService.Get', [])`).
package appinfo

import (
	"encoding/json"

	"github.com/hkdb/flugo/pkg/bridge"
)

// Set by the linker (-X github.com/hkdb/flugo/pkg/appinfo.<var>=...).
var (
	version = "dev"
	name    = ""
	id      = ""
)

// Version is the app version from flugo.yaml, or "dev" when unstamped.
func Version() string { return version }

// Name is the app name from flugo.yaml, or "" when unstamped.
func Name() string { return name }

// ID is the app id from flugo.yaml, or "" when unstamped.
func ID() string { return id }

// flugoAppInfoService is registered as a built-in bridge service.
type flugoAppInfoService struct{}

// Get returns the stamped identity as JSON: {"name":…,"id":…,"version":…}.
func (s *flugoAppInfoService) Get() (string, error) {
	b, err := json.Marshal(map[string]string{"name": name, "id": id, "version": version})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func init() {
	bridge.Bind(&flugoAppInfoService{})
}
