package scaffold

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderMainActivity(t *testing.T) {
	frontend := t.TempDir()
	kotlin := filepath.Join(frontend, "android", "app", "src", "main", "kotlin", "com", "example", "demo")
	if err := os.MkdirAll(kotlin, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := "package com.example.demo\n\nimport io.flutter.embedding.android.FlutterActivity\n\nclass MainActivity : FlutterActivity()\n"
	if err := os.WriteFile(filepath.Join(kotlin, "MainActivity.kt"), []byte(stub), 0o644); err != nil {
		t.Fatal(err)
	}

	path, content, err := renderMainActivity(frontend)
	if err != nil {
		t.Fatalf("renderMainActivity: %v", err)
	}
	if path == "" {
		t.Fatal("expected a MainActivity path")
	}
	s := string(content)
	for _, want := range []string{
		"package com.example.demo",
		`System.loadLibrary("backend")`,
		"DocumentsContract", // SAF Open Folder
		"MethodChannel",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rendered MainActivity missing %q", want)
		}
	}
}

func TestRenderMainActivity_NoAndroid(t *testing.T) {
	// A desktop-only project (no android/ tree) must be skipped, not errored.
	if _, _, err := renderMainActivity(t.TempDir()); !errors.Is(err, errNoAndroidMainActivity) {
		t.Fatalf("want errNoAndroidMainActivity, got %v", err)
	}
}
