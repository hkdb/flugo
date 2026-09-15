package version

import (
	"strings"
	"testing"
)

func TestBaseIsTrimmedNonEmptySemver(t *testing.T) {
	b := Base()
	if b == "" {
		t.Fatal("Base() is empty — VERSION file missing or blank")
	}
	if b != strings.TrimSpace(b) {
		t.Errorf("Base() not trimmed: %q", b)
	}
	if strings.HasPrefix(b, "v") {
		t.Errorf("Base() should be bare (no leading v): %q", b)
	}
}

func TestTagPrefixesV(t *testing.T) {
	if got, want := Tag(), "v"+Base(); got != want {
		t.Errorf("Tag() = %q, want %q", got, want)
	}
}

func TestDisplay(t *testing.T) {
	base := Base()
	cases := []struct {
		name string
		bi   buildInfo
		want string
	}{
		{"release install (no vcs)", buildInfo{}, base},
		{"local build with commit", buildInfo{revision: "abcdef1234567890"}, base + "-abcdef123456"},
		{"local dirty build", buildInfo{revision: "abcdef1234567890", modified: true}, base + "-abcdef123456-dirty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := display(func() buildInfo { return tc.bi })
			if got != tc.want {
				t.Errorf("display() = %q, want %q", got, tc.want)
			}
		})
	}
}
