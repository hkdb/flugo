package builder

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestAppImageToolSHA256WellFormed(t *testing.T) {
	if len(appImageToolSHA256) == 0 {
		t.Fatal("appImageToolSHA256 is empty")
	}
	hexre := regexp.MustCompile(`^[a-f0-9]{64}$`)
	for arch, sum := range appImageToolSHA256 {
		if !hexre.MatchString(sum) {
			t.Errorf("arch %s: SHA256 is not a 64-char lowercase hex digest: %q", arch, sum)
		}
	}
}

// TestAppImageToolSHA256MatchesUpstream guards against a stale checksum after a
// version bump: it fetches the pinned appimagetool release and asserts every
// embedded digest equals the published asset digest. Network-dependent —
// skipped under -short or when GitHub is unreachable/omits digests.
func TestAppImageToolSHA256MatchesUpstream(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network check in -short mode")
	}
	client := &http.Client{Timeout: 20 * time.Second}
	url := "https://api.github.com/repos/AppImage/appimagetool/releases/tags/" + appImageToolVersion
	resp, err := client.Get(url)
	if err != nil {
		t.Skipf("GitHub unreachable, skipping upstream checksum check: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("GitHub returned %d, skipping upstream checksum check", resp.StatusCode)
	}

	var rel struct {
		Assets []struct {
			Name   string `json:"name"`
			Digest string `json:"digest"` // "sha256:<hex>"
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		t.Skipf("decoding GitHub response failed, skipping: %v", err)
	}

	upstream := map[string]string{} // arch -> sha256
	for _, a := range rel.Assets {
		for arch := range appImageToolSHA256 {
			if a.Name == "appimagetool-"+arch+".AppImage" {
				upstream[arch] = strings.TrimPrefix(a.Digest, "sha256:")
			}
		}
	}

	for arch, want := range appImageToolSHA256 {
		got, ok := upstream[arch]
		if !ok {
			t.Errorf("appimagetool-%s.AppImage not found in release %s", arch, appImageToolVersion)
			continue
		}
		if got == "" {
			t.Skipf("GitHub omitted a digest for %s; skipping compare", arch)
		}
		if got != want {
			t.Errorf("arch %s: embedded SHA256 is stale: got %q, upstream %q — update appImageToolSHA256", arch, want, got)
		}
	}
}
