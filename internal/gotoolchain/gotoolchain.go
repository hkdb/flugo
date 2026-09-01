// Package gotoolchain pins the Go toolchain that Flatpak/Flathub builds fetch
// as archive sources to compile the backend inside the sandbox — one archive per
// target architecture, selected at build time via the Flatpak `only-arches` key.
//
// Version and every Arch.SHA256 must be kept in sync with each other (and with
// the backend's go.mod) on every bump: update them together.
// TestSHA256MatchesUpstream verifies each checksum against the official Go
// download list when run online.
package gotoolchain

import "fmt"

// Version is the pinned Go toolchain version (matches backend go.mod).
const Version = "1.26.6"

// Arch describes one target architecture's Go toolchain archive.
type Arch struct {
	// Go is the GOARCH used in the go.dev archive name (e.g. "amd64", "arm64").
	Go string
	// Flatpak is the Flatpak arch name used in `only-arches` (e.g. "x86_64",
	// "aarch64").
	Flatpak string
	// SHA256 is the checksum of the archive, from https://go.dev/dl/.
	SHA256 string
}

// Arches lists the toolchain archives shipped in the Flatpak manifest, one per
// supported target architecture. Keep each SHA256 in sync with Version.
var Arches = []Arch{
	{Go: "amd64", Flatpak: "x86_64", SHA256: "708effb774be8237570d0add163225abbdfaf4fca28b2611df167beba4feef89"},
	{Go: "arm64", Flatpak: "aarch64", SHA256: "d0507e9e9d7fe012aae570108cbd76c15de879e17130ab8cb90d4d7445cb1f2e"},
}

// ArchiveName returns the toolchain archive filename for this arch, e.g.
// "go1.26.1.linux-amd64.tar.gz".
func (a Arch) ArchiveName() string {
	return fmt.Sprintf("go%s.linux-%s.tar.gz", Version, a.Go)
}

// ArchiveURL returns the go.dev download URL for this arch's toolchain archive.
func (a Arch) ArchiveURL() string {
	return "https://go.dev/dl/" + a.ArchiveName()
}
