package scaffold

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// testBlock builds a flugo-managed block from the given ignore paths.
func testBlock(paths ...string) string {
	return gitignoreBlockStart + "\n" + strings.Join(paths, "\n") + "\n" + gitignoreBlockEnd
}

// currentBlock mirrors what gitignore.tmpl renders today (corrected paths).
var currentBlock = testBlock(
	"backend/bridge.gen.go",
	"backend/bridge/bridge.gen.h",
	"frontend/lib/bridge/bridge.gen.dart",
)

func TestExtractManagedBlock(t *testing.T) {
	rendered := "# Go\n*.exe\n\n" + currentBlock + "\n\n# IDE\n.idea/\n"
	got, err := extractManagedBlock([]byte(rendered))
	if err != nil {
		t.Fatalf("extractManagedBlock: %v", err)
	}
	if got != currentBlock {
		t.Errorf("extracted block mismatch:\n got %q\nwant %q", got, currentBlock)
	}
	if _, err := extractManagedBlock([]byte("no markers here\n")); err == nil {
		t.Error("expected an error when the markers are absent")
	}
}

func TestMergeGitignore_AppendsWhenNoMarkers(t *testing.T) {
	existing := "# Go\n*.exe\n\n# custom\nmy-secret-file\n"
	merged := string(mergeGitignore([]byte(existing), currentBlock))
	if !strings.HasPrefix(merged, existing) {
		t.Error("existing content not preserved verbatim at the start")
	}
	if !strings.Contains(merged, "my-secret-file") {
		t.Error("developer line was lost")
	}
	if !strings.Contains(merged, currentBlock) {
		t.Error("managed block was not appended")
	}
}

func TestMergeGitignore_ReplacesStaleBlockPreservingDevLines(t *testing.T) {
	stale := testBlock(
		"backend/bridge/bridge.gen.go", // old, wrong location
		"backend/bridge/bridge.gen.h",
		"frontend/lib/bridge/bridge.gen.dart",
	)
	existing := "# Go\n*.exe\n\n" + stale + "\n\n# custom\nmy-secret-file\n"
	merged := string(mergeGitignore([]byte(existing), currentBlock))

	if !strings.Contains(merged, "\nbackend/bridge.gen.go\n") {
		t.Error("corrected bridge.gen.go path missing after refresh")
	}
	if strings.Contains(merged, "backend/bridge/bridge.gen.go") {
		t.Error("stale bridge/bridge.gen.go path was not replaced")
	}
	if !strings.Contains(merged, "my-secret-file") {
		t.Error("developer line lost during in-place replace")
	}
	if !strings.Contains(merged, "*.exe") {
		t.Error("content before the block was lost")
	}
	if strings.Count(merged, gitignoreBlockStart) != 1 {
		t.Error("managed block was duplicated instead of replaced")
	}
}

func TestMergeGitignore_UnchangedWhenCurrent(t *testing.T) {
	existing := "# Go\n*.exe\n\n" + currentBlock + "\n\n# custom\nfoo\n"
	merged := mergeGitignore([]byte(existing), currentBlock)
	if !bytes.Equal(merged, []byte(existing)) {
		t.Errorf("already-current merge must be byte-identical (drives the 'unchanged' report):\n got %q\nwant %q", merged, existing)
	}
}

func TestMergeGitignore_EmptyExisting(t *testing.T) {
	if got := string(mergeGitignore([]byte(""), currentBlock)); got != currentBlock+"\n" {
		t.Errorf("empty-existing merge = %q, want block + newline", got)
	}
}

// TestMergeGitignore_TemplateRoundTrips guards the real template: a freshly
// scaffolded .gitignore IS the template, so extracting its block and merging it
// back in must be a no-op — otherwise `flugo update` would report every fresh
// project's .gitignore as changed. Also pins the corrected bridge.gen.go path.
func TestMergeGitignore_TemplateRoundTrips(t *testing.T) {
	tmplBytes, err := os.ReadFile("templates/gitignore.tmpl")
	if err != nil {
		t.Fatalf("reading gitignore.tmpl: %v", err)
	}
	block, err := extractManagedBlock(tmplBytes)
	if err != nil {
		t.Fatalf("extract from real template: %v", err)
	}
	if merged := mergeGitignore(tmplBytes, block); !bytes.Equal(merged, tmplBytes) {
		t.Errorf("template does not round-trip through merge (fresh projects would show 'updated'):\n got %q\nwant %q", merged, tmplBytes)
	}
	if !strings.Contains(block, "\nbackend/bridge.gen.go\n") || strings.Contains(block, "backend/bridge/bridge.gen.go") {
		t.Errorf("template block has the wrong bridge.gen.go path:\n%s", block)
	}
}
