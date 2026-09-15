// Package version resolves the flugo CLI's version.
//
// The base version comes from a committed VERSION file embedded at build time
// (go:embed), so it is reliable for ANY build method: a local `go build`, a
// `./install.sh`, and a released `go install …@vX.Y.Z` all report the same
// base. This replaces sniffing the module version out of debug.ReadBuildInfo,
// which is only populated for tagged installs and reports "(devel)" for local
// builds. Bump VERSION before a release (alongside cutting the matching git
// tag, which is only needed so consumers' `go.mod require` resolves).
package version

import (
	_ "embed"
	"runtime/debug"
	"strings"
)

//go:embed VERSION
var versionFile string

// Base returns the bare, resolvable base version, e.g. "0.2.1". This is the
// value stamped into consumers (flugo.yaml, backend/go.mod, pubspec refs) and
// compared for drift — it must always be a valid, fetchable semver.
func Base() string {
	return strings.TrimSpace(versionFile)
}

// Tag returns the base version in git/go-module tag form, e.g. "v0.2.1".
func Tag() string {
	return "v" + Base()
}

// Display returns the human-facing version for `flugo version` and drift hints.
// A released install shows the bare base; a local/untagged build appends the
// build commit (and "-dirty" if the tree was modified) so it is identifiable
// and never mistaken for a release. The commit suffix is identity-only and is
// never used for stamping/pinning.
func Display() string {
	return display(readBuildInfo)
}

// buildInfo is the subset of debug.BuildInfo this package reads. It is a seam
// so display() can be unit-tested without a real VCS-stamped build.
type buildInfo struct {
	revision string // vcs.revision — present only for a build from a checkout
	modified bool   // vcs.modified
}

func readBuildInfo() buildInfo {
	bi := buildInfo{}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return bi
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			bi.revision = s.Value
		case "vcs.modified":
			bi.modified = s.Value == "true"
		}
	}
	return bi
}

func display(read func() buildInfo) string {
	base := Base()
	bi := read()
	// A build from a git checkout (local: `go build`/`go install ./cmd/flugo`)
	// records vcs.revision; a released `go install …@vX.Y.Z` (built from the
	// module cache) does not. So the presence of a revision is the reliable
	// local-vs-release signal — Main.Version is unreliable (a local dirty build
	// reports e.g. "v0.2.0+dirty", not "(devel)").
	if bi.revision == "" {
		return base
	}
	rev := bi.revision
	if len(rev) > 12 {
		rev = rev[:12]
	}
	out := base + "-" + rev
	if bi.modified {
		out += "-dirty"
	}
	return out
}
