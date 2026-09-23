package appinfo

import (
	"encoding/json"
	"testing"
)

func TestUnstampedDefaults(t *testing.T) {
	if Version() != "dev" || Name() != "" || ID() != "" {
		t.Fatalf("unstamped build must report dev/empty, got %q %q %q", Version(), Name(), ID())
	}
	out, err := (&flugoAppInfoService{}).Get()
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got["version"] != "dev" || got["name"] != "" || got["id"] != "" {
		t.Fatalf("bridge view must match the accessors: %v", got)
	}
}
