// Package flathub generates all assets needed for a Flathub submission.
package flathub

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
	"time"

	"github.com/hkdb/flugo/internal/config"
	"github.com/hkdb/flugo/internal/gotoolchain"
)

type templateData struct {
	AppID          string
	AppName        string
	Name           string
	Description    string
	Version        string
	License        string
	URL            string
	Date           string
	Runtime        string
	RuntimeVersion string
	SDK            string
	Permissions    []string
	URLScheme      string
	GoArches       []gotoolchain.Arch
}

// Generate creates Flathub submission assets in assets/linux/.
func Generate(projectDir string, cfg *config.Config) error {
	fmt.Println("Generating Flathub assets...")

	if err := cfg.ValidateForGeneration(); err != nil {
		return err
	}

	outputDir := filepath.Join(projectDir, "assets", "linux")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}

	// Derive short name from app ID (last component).
	name := cfg.App.ID
	parts := splitAppID(name)
	if len(parts) > 0 {
		name = parts[len(parts)-1]
	}

	data := templateData{
		AppID:          cfg.App.ID,
		AppName:        cfg.App.Name,
		Name:           name,
		Description:    cfg.App.Description,
		Version:        cfg.App.Version,
		License:        cfg.App.License,
		URL:            cfg.App.URL,
		Date:           time.Now().Format("2006-01-02"),
		Runtime:        cfg.Platforms.Linux.Flatpak.Runtime,
		RuntimeVersion: cfg.Platforms.Linux.Flatpak.RuntimeVersion,
		SDK:            cfg.Platforms.Linux.Flatpak.SDK,
		Permissions:    cfg.Platforms.Linux.Flatpak.Permissions,
		URLScheme:      cfg.App.URLScheme,
		GoArches:       gotoolchain.Arches,
	}

	if data.Runtime == "" {
		data.Runtime = "org.freedesktop.Platform"
	}
	if data.RuntimeVersion == "" {
		data.RuntimeVersion = "24.08"
	}
	if data.SDK == "" {
		data.SDK = "org.freedesktop.Sdk"
	}

	// Generate desktop file.
	if err := writeTemplate(desktopTmpl, data, filepath.Join(outputDir, cfg.App.ID+".desktop")); err != nil {
		return fmt.Errorf("generating .desktop: %w", err)
	}

	// Generate metainfo XML.
	if err := writeTemplate(metainfoTmpl, data, filepath.Join(outputDir, cfg.App.ID+".metainfo.xml")); err != nil {
		return fmt.Errorf("generating metainfo: %w", err)
	}

	// Generate Flatpak manifest.
	if err := writeTemplate(manifestTmpl, data, filepath.Join(outputDir, cfg.App.ID+".yaml")); err != nil {
		return fmt.Errorf("generating manifest: %w", err)
	}

	fmt.Printf("Generated:\n")
	fmt.Printf("  %s.desktop\n", cfg.App.ID)
	fmt.Printf("  %s.metainfo.xml\n", cfg.App.ID)
	fmt.Printf("  %s.yaml\n", cfg.App.ID)

	// Validate with appstreamcli if available.
	metainfoPath := filepath.Join(outputDir, cfg.App.ID+".metainfo.xml")
	if appstreamcli, err := exec.LookPath("appstreamcli"); err == nil {
		fmt.Println("\nValidating metainfo...")
		cmd := exec.Command(appstreamcli, "validate", metainfoPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		runErr := cmd.Run()
		if runErr != nil {
			fmt.Println("Warning: metainfo validation had issues (see above)")
		}
		if runErr == nil {
			fmt.Println("Metainfo validation passed!")
		}
	}

	return nil
}

func writeTemplate(tmplStr string, data templateData, outputPath string) error {
	tmpl, err := template.New("").Parse(tmplStr)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}

	return os.WriteFile(outputPath, buf.Bytes(), 0o644)
}

func splitAppID(id string) []string {
	var parts []string
	current := ""
	for _, c := range id {
		if c == '.' {
			if current != "" {
				parts = append(parts, current)
			}
			current = ""
			continue
		}
		current += string(c)
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

const desktopTmpl = `[Desktop Entry]
Type=Application
Name={{.AppName}}
Comment={{.Description}}
Exec={{.Name}}{{if .URLScheme}} %u{{end}}
Icon={{.AppID}}
Terminal=false
Categories=Utility;
{{- if .URLScheme}}
MimeType=x-scheme-handler/{{.URLScheme}};
{{- end}}
`

const metainfoTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<component type="desktop-application">
  <id>{{.AppID}}</id>
  <name>{{.AppName}}</name>
  <summary>{{.Description}}</summary>
  <metadata_license>CC0-1.0</metadata_license>
{{- if .License}}
  <project_license>{{.License}}</project_license>
{{- end}}
  <description>
    <p>{{.Description}}</p>
  </description>
  <launchable type="desktop-id">{{.AppID}}.desktop</launchable>
{{- if .URL}}
  <url type="homepage">{{.URL}}</url>
{{- end}}
  <provides>
    <binary>{{.Name}}</binary>
  </provides>
  <content_rating type="oars-1.1" />
  <releases>
    <release version="{{.Version}}" date="{{.Date}}" />
  </releases>
</component>
`

const manifestTmpl = `id: {{.AppID}}
runtime: {{.Runtime}}
runtime-version: '{{.RuntimeVersion}}'
sdk: {{.SDK}}
command: {{.Name}}
finish-args:
  - --share=ipc
  - --socket=fallback-x11
  - --socket=wayland
  - --device=dri
{{- range .Permissions}}
  - {{.}}
{{- end}}

modules:
  - name: golang
    buildsystem: simple
    build-commands:
      - install -d /app/go
      - cp -r . /app/go
    sources:
{{- range .GoArches}}
      - type: archive
        url: {{.ArchiveURL}}
        sha256: {{.SHA256}}
        only-arches:
          - {{.Flatpak}}
        dest: .
{{- end}}

  - name: {{.Name}}
    buildsystem: simple
    build-commands:
      - export PATH=/app/go/bin:$PATH
      - cd backend && CGO_ENABLED=1 go build -buildmode=c-shared -o /app/lib/libbackend.so .
      - flutter build linux --release
      - cp -r frontend/build/linux/*/release/bundle/* /app/
    sources:
      - type: dir
        path: .
`
