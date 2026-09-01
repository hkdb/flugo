package flathub

import (
	"bytes"
	"strings"
	"testing"
	"text/template"

	"github.com/hkdb/flugo/internal/gotoolchain"
)

// TestManifestHasRealChecksum ensures the generated Flatpak manifest carries the
// pinned Go toolchain URL + real checksum, not the old `sha256: FIXME` placeholder
// that made flatpak-builder reject the build.
func TestManifestHasRealChecksum(t *testing.T) {
	data := templateData{
		AppID:          "io.example.App",
		Name:           "app",
		Runtime:        "org.freedesktop.Platform",
		RuntimeVersion: "24.08",
		SDK:            "org.freedesktop.Sdk",
		GoArches:       gotoolchain.Arches,
	}

	tmpl := template.Must(template.New("").Parse(manifestTmpl))
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("rendering manifest: %v", err)
	}
	out := buf.String()

	if strings.Contains(out, "FIXME") {
		t.Errorf("manifest still contains a FIXME placeholder:\n%s", out)
	}
	for _, a := range gotoolchain.Arches {
		if !strings.Contains(out, "sha256: "+a.SHA256) {
			t.Errorf("manifest missing the %s checksum:\n%s", a.Go, out)
		}
		if !strings.Contains(out, a.ArchiveURL()) {
			t.Errorf("manifest missing the %s toolchain URL:\n%s", a.Go, out)
		}
		if !strings.Contains(out, "- "+a.Flatpak) {
			t.Errorf("manifest missing only-arches entry for %s:\n%s", a.Flatpak, out)
		}
	}
}
