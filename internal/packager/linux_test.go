package packager

import (
	"os/exec"
	"testing"
)

func TestShQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{`Instacrypt`, `'Instacrypt'`},
		{``, `''`},
		{`My$App`, `'My$App'`},
		{"back`tick`", "'back`tick`'"},
		{`cmd$(id)`, `'cmd$(id)'`},
		{`a"b`, `'a"b'`},
		{`it's`, `'it'\''s'`},
	}
	for _, c := range cases {
		if got := shQuote(c.in); got != c.want {
			t.Errorf("shQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestShQuoteSourceRoundTrip proves a quoted value survives `source` as literal
// text and executes nothing — the core of the F1 hardening. Skips if /bin/sh is
// unavailable.
func TestShQuoteSourceRoundTrip(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not found")
	}
	// If quoting failed, $(...)/backtick/$VAR would be expanded or executed and
	// the printed value would differ from the literal input — so an exact
	// round-trip proves nothing was interpreted by the shell.
	for _, in := range []string{`Instacrypt`, `My$App`, `cmd$(echo x)`, "back`echo x`", `it's a "name"`} {
		script := "APP_NAME=" + shQuote(in) + "\nprintf %s \"$APP_NAME\"\n"
		out, err := exec.Command(sh, "-c", script).Output()
		if err != nil {
			t.Fatalf("sh failed for %q: %v", in, err)
		}
		if got := string(out); got != in {
			t.Errorf("round-trip of %q = %q (shell interpreted the value)", in, got)
		}
	}
}
