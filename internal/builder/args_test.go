package builder

import (
	"strings"
	"testing"

	"github.com/hkdb/flugo/internal/config"
)

func TestGoBuildArgsStampAppInfo(t *testing.T) {
	cfg := &config.Config{}
	cfg.App.Name = "Demo App"
	cfg.App.ID = "io.example.demo"
	cfg.App.Version = "1.2.3"
	b := New(t.TempDir(), cfg)

	args := b.goBuildArgs("c-shared", "/out/libbackend.so")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"build -buildmode=c-shared -ldflags ",
		"-X '" + appInfoPkg + ".version=1.2.3'",
		"-X '" + appInfoPkg + ".name=Demo App'",
		"-X '" + appInfoPkg + ".id=io.example.demo'",
		" -o /out/libbackend.so .",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
	if args[len(args)-1] != "." || args[len(args)-3] != "-o" {
		t.Fatalf("output and package must be the trailing args: %q", args)
	}
}
