package scaffold

import (
	"bytes"
	"strings"
	"testing"
	"text/template"

	"github.com/hkdb/flugo/internal/gotoolchain"
)

// TestScaffoldedFlatpakManifestHasRealChecksum ensures the scaffolded/updated
// Flatpak manifest (assets/linux/<AppID>.yaml) carries the pinned Go toolchain
// URL + real checksum rather than the old `sha256: FIXME` placeholder.
func TestScaffoldedFlatpakManifestHasRealChecksum(t *testing.T) {
	raw, err := templateFS.ReadFile("templates/flatpak_manifest.tmpl")
	if err != nil {
		t.Fatalf("reading embedded template: %v", err)
	}

	data := templateData{
		AppID:          "io.example.App",
		Name:           "app",
		Runtime:        "org.freedesktop.Platform",
		RuntimeVersion: "24.08",
		SDK:            "org.freedesktop.Sdk",
		GoArches:       gotoolchain.Arches,
	}

	tmpl := template.Must(template.New("").Parse(string(raw)))
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
