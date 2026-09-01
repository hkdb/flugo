package gotoolchain

import (
	"encoding/json"
	"net/http"
	"regexp"
	"testing"
	"time"
)

func TestArchesWellFormed(t *testing.T) {
	if len(Arches) == 0 {
		t.Fatal("Arches is empty")
	}
	hex := regexp.MustCompile(`^[a-f0-9]{64}$`)
	seen := map[string]bool{}
	for _, a := range Arches {
		if a.Go == "" || a.Flatpak == "" {
			t.Errorf("arch %+v has an empty Go/Flatpak name", a)
		}
		if seen[a.Flatpak] {
			t.Errorf("duplicate Flatpak arch %q", a.Flatpak)
		}
		seen[a.Flatpak] = true
		if !hex.MatchString(a.SHA256) {
			t.Errorf("arch %s: SHA256 is not a 64-char lowercase hex digest: %q", a.Go, a.SHA256)
		}
		wantName := "go" + Version + ".linux-" + a.Go + ".tar.gz"
		if got := a.ArchiveName(); got != wantName {
			t.Errorf("ArchiveName() = %q, want %q", got, wantName)
		}
		if got, want := a.ArchiveURL(), "https://go.dev/dl/"+wantName; got != want {
			t.Errorf("ArchiveURL() = %q, want %q", got, want)
		}
	}
}

// TestSHA256MatchesUpstream guards against a stale checksum after a Version bump:
// it fetches the official Go download list and asserts every embedded Arch.SHA256
// equals the published checksum for go<Version>.linux-<arch>.tar.gz. Network-
// dependent — skipped under -short or when go.dev is unreachable.
func TestSHA256MatchesUpstream(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network check in -short mode")
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get("https://go.dev/dl/?mode=json&include=all")
	if err != nil {
		t.Skipf("go.dev unreachable, skipping upstream checksum check: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("go.dev returned %d, skipping upstream checksum check", resp.StatusCode)
	}

	var releases []struct {
		Version string `json:"version"`
		Files   []struct {
			OS     string `json:"os"`
			Arch   string `json:"arch"`
			Kind   string `json:"kind"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		t.Skipf("decoding go.dev response failed, skipping: %v", err)
	}

	// Build a lookup of upstream archive checksums for our pinned version.
	upstream := map[string]string{} // GOARCH -> sha256
	for _, rel := range releases {
		if rel.Version != "go"+Version {
			continue
		}
		for _, f := range rel.Files {
			if f.OS == "linux" && f.Kind == "archive" {
				upstream[f.Arch] = f.SHA256
			}
		}
	}

	for _, a := range Arches {
		want, ok := upstream[a.Go]
		if !ok {
			t.Errorf("go%s linux-%s archive not found upstream — is the pinned Version published for this arch?", Version, a.Go)
			continue
		}
		if want != a.SHA256 {
			t.Errorf("arch %s: embedded SHA256 is stale: got %q, upstream %q — update gotoolchain.Arches", a.Go, a.SHA256, want)
		}
	}
}
