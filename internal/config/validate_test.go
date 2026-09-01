package config

import "testing"

func baseValidConfig() *Config {
	c := &Config{}
	c.App.ID = "io.github.instacryptio.ic-app" // hyphen in last segment must be allowed
	c.App.Description = "A fast & secure app"
	c.Platforms.Linux.Flatpak.Permissions = []string{"--share=network", "--socket=wayland", "--filesystem=home"}
	return c
}

func TestValidateForGeneration_Accepts(t *testing.T) {
	if err := baseValidConfig().ValidateForGeneration(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestValidateForGeneration_Rejects(t *testing.T) {
	t.Run("bad app.id", func(t *testing.T) {
		c := baseValidConfig()
		c.App.ID = "not a valid id"
		if err := c.ValidateForGeneration(); err == nil {
			t.Error("expected rejection of malformed app.id")
		}
	})
	t.Run("single-segment app.id", func(t *testing.T) {
		c := baseValidConfig()
		c.App.ID = "myapp"
		if err := c.ValidateForGeneration(); err == nil {
			t.Error("expected rejection of non-dotted app.id")
		}
	})
	t.Run("newline in description", func(t *testing.T) {
		c := baseValidConfig()
		c.App.Description = "line1\ninjected: true"
		if err := c.ValidateForGeneration(); err == nil {
			t.Error("expected rejection of newline in description")
		}
	})
	t.Run("newline in permission", func(t *testing.T) {
		c := baseValidConfig()
		c.Platforms.Linux.Flatpak.Permissions = []string{"--share=network\n  extra-key: bad"}
		if err := c.ValidateForGeneration(); err == nil {
			t.Error("expected rejection of newline in a flatpak permission")
		}
	})
}
