// Package config handles parsing and validation of flugo.yaml project configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// appIDRe validates app.id: reverse-DNS-style dotted segments. Hyphens are
// allowed within a segment (some IDs use them, e.g. ...ic-app). This blocks
// whitespace, slashes, newlines, and path-traversal from a field that becomes
// filenames + YAML/XML ids in generated packaging.
var appIDRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*(\.[A-Za-z][A-Za-z0-9_-]*)+$`)

// ValidateForGeneration checks developer-supplied config values that get
// interpolated into generated packaging files (Flatpak manifest YAML, .desktop,
// metainfo XML) via text/template. It rejects values that would corrupt or
// inject into those files — a newline/control char, or a malformed app.id.
func (c *Config) ValidateForGeneration() error {
	if !appIDRe.MatchString(c.App.ID) {
		return fmt.Errorf("invalid app.id %q: expected reverse-DNS like com.example.app", c.App.ID)
	}
	if hasControlOrNewline(c.App.Description) {
		return fmt.Errorf("app.description must not contain newlines or control characters")
	}
	for i, p := range c.Platforms.Linux.Flatpak.Permissions {
		if hasControlOrNewline(p) {
			return fmt.Errorf("flatpak permission #%d (%q) must not contain newlines or control characters", i+1, p)
		}
	}
	return nil
}

func hasControlOrNewline(s string) bool {
	for _, r := range s {
		if r == '\n' || r == '\r' || r == 0x7f || (r < 0x20 && r != '\t') {
			return true
		}
	}
	return false
}

// Config represents the top-level flugo.yaml configuration.
type Config struct {
	FlugoVersion string         `yaml:"flugo_version,omitempty"`
	App          AppConfig      `yaml:"app"`
	Backend      BackendConfig  `yaml:"backend"`
	Platforms    PlatformConfig `yaml:"platforms"`
	Icons        IconsConfig    `yaml:"icons"`
}

// AppConfig holds application metadata.
type AppConfig struct {
	ID          string       `yaml:"id"`
	Name        string       `yaml:"name"`
	Version     string       `yaml:"version"`
	Description string       `yaml:"description"`
	Author      string       `yaml:"author"`
	License     string       `yaml:"license"`
	URL         string       `yaml:"url"`
	// URLScheme enables deep linking: the app registers as the handler for
	// <url_scheme>:// links on every platform (run `flugo deeplink` after
	// setting it on an existing project). Lowercase letters only.
	URLScheme string       `yaml:"url_scheme,omitempty"`
	Window    WindowConfig `yaml:"window"`
}

// WindowConfig holds window size defaults for desktop platforms.
type WindowConfig struct {
	Width         int    `yaml:"width"`
	Height        int    `yaml:"height"`
	MinWidth      int    `yaml:"min_width"`
	MinHeight     int    `yaml:"min_height"`
	TitlebarStyle string `yaml:"titlebar_style"`
}

// BackendConfig points to the Go backend.
type BackendConfig struct {
	Module   string   `yaml:"module"`
	Services []string `yaml:"services"`
}

// PlatformConfig holds per-platform settings.
type PlatformConfig struct {
	Linux   LinuxConfig   `yaml:"linux"`
	MacOS   MacOSConfig   `yaml:"macos"`
	Windows WindowsConfig `yaml:"windows"`
	Android AndroidConfig `yaml:"android"`
	IOS     IOSConfig     `yaml:"ios"`
}

// LinuxConfig holds Linux-specific settings.
type LinuxConfig struct {
	Flatpak FlatpakConfig `yaml:"flatpak"`
}

// FlatpakConfig holds Flatpak packaging settings.
type FlatpakConfig struct {
	Runtime        string   `yaml:"runtime"`
	RuntimeVersion string   `yaml:"runtime_version"`
	SDK            string   `yaml:"sdk"`
	Permissions    []string `yaml:"permissions"`
}

// MacOSConfig holds macOS-specific settings.
type MacOSConfig struct {
	BundleID       string `yaml:"bundle_id"`
	MinimumVersion string `yaml:"minimum_version"`
	Sandbox        bool   `yaml:"sandbox"`
}

// WindowsConfig holds Windows-specific settings.
type WindowsConfig struct {
	Publisher string `yaml:"publisher"`
}

// AndroidConfig holds Android-specific settings.
type AndroidConfig struct {
	MinSDK    int `yaml:"min_sdk"`
	TargetSDK int `yaml:"target_sdk"`
}

// IOSConfig holds iOS-specific settings.
type IOSConfig struct {
	MinimumVersion string `yaml:"minimum_version"`
}

// IconsConfig points to the source icon.
type IconsConfig struct {
	Source string `yaml:"source"`
}

// Load reads and parses flugo.yaml from the given project directory.
func Load(projectDir string) (*Config, error) {
	path := filepath.Join(projectDir, "flugo.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading flugo.yaml: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing flugo.yaml: %w", err)
	}

	return &cfg, nil
}

// Save writes the config back to flugo.yaml in the given project directory.
// It uses read-modify-write to preserve comments and field ordering where possible.
func Save(projectDir string, cfg *Config) error {
	path := filepath.Join(projectDir, "flugo.yaml")

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling flugo.yaml: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing flugo.yaml: %w", err)
	}

	return nil
}

// FindProjectRoot walks up from dir looking for flugo.yaml.
func FindProjectRoot(dir string) (string, error) {
	for {
		if _, err := os.Stat(filepath.Join(dir, "flugo.yaml")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("flugo.yaml not found (searched up from working directory)")
		}
		dir = parent
	}
}
