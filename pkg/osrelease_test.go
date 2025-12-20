package pkg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseOSRelease_WithPrettyName(t *testing.T) {
	tmpDir := t.TempDir()
	etcDir := filepath.Join(tmpDir, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatalf("Failed to create etc directory: %v", err)
	}

	osReleaseContent := `NAME="Test Linux"
VERSION="1.0"
ID=testlinux
PRETTY_NAME="Test Linux 1.0 (Amazing)"
`
	osReleasePath := filepath.Join(etcDir, "os-release")
	if err := os.WriteFile(osReleasePath, []byte(osReleaseContent), 0644); err != nil {
		t.Fatalf("Failed to write os-release: %v", err)
	}

	result := ParseOSRelease(tmpDir)
	expected := "Test Linux 1.0 (Amazing)"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestParseOSRelease_WithNameOnly(t *testing.T) {
	tmpDir := t.TempDir()
	etcDir := filepath.Join(tmpDir, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatalf("Failed to create etc directory: %v", err)
	}

	osReleaseContent := `NAME="Test Linux"
VERSION="1.0"
ID=testlinux
`
	osReleasePath := filepath.Join(etcDir, "os-release")
	if err := os.WriteFile(osReleasePath, []byte(osReleaseContent), 0644); err != nil {
		t.Fatalf("Failed to write os-release: %v", err)
	}

	result := ParseOSRelease(tmpDir)
	expected := "Test Linux"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestParseOSRelease_WithIDOnly(t *testing.T) {
	tmpDir := t.TempDir()
	etcDir := filepath.Join(tmpDir, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatalf("Failed to create etc directory: %v", err)
	}

	osReleaseContent := `ID=testlinux
VERSION="1.0"
`
	osReleasePath := filepath.Join(etcDir, "os-release")
	if err := os.WriteFile(osReleasePath, []byte(osReleaseContent), 0644); err != nil {
		t.Fatalf("Failed to write os-release: %v", err)
	}

	result := ParseOSRelease(tmpDir)
	expected := "testlinux"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestParseOSRelease_FallbackLocation(t *testing.T) {
	tmpDir := t.TempDir()
	usrLibDir := filepath.Join(tmpDir, "usr", "lib")
	if err := os.MkdirAll(usrLibDir, 0755); err != nil {
		t.Fatalf("Failed to create usr/lib directory: %v", err)
	}

	osReleaseContent := `PRETTY_NAME="Fallback Test Linux"
ID=fallbacktest
`
	osReleasePath := filepath.Join(usrLibDir, "os-release")
	if err := os.WriteFile(osReleasePath, []byte(osReleaseContent), 0644); err != nil {
		t.Fatalf("Failed to write os-release: %v", err)
	}

	result := ParseOSRelease(tmpDir)
	expected := "Fallback Test Linux"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestParseOSRelease_NoFile(t *testing.T) {
	tmpDir := t.TempDir()

	result := ParseOSRelease(tmpDir)
	expected := "Linux"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestParseOSRelease_SingleQuotes(t *testing.T) {
	tmpDir := t.TempDir()
	etcDir := filepath.Join(tmpDir, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatalf("Failed to create etc directory: %v", err)
	}

	osReleaseContent := `PRETTY_NAME='Test Linux with Single Quotes'
ID=testlinux
`
	osReleasePath := filepath.Join(etcDir, "os-release")
	if err := os.WriteFile(osReleasePath, []byte(osReleaseContent), 0644); err != nil {
		t.Fatalf("Failed to write os-release: %v", err)
	}

	result := ParseOSRelease(tmpDir)
	expected := "Test Linux with Single Quotes"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestParseOSRelease_NoQuotes(t *testing.T) {
	tmpDir := t.TempDir()
	etcDir := filepath.Join(tmpDir, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatalf("Failed to create etc directory: %v", err)
	}

	osReleaseContent := `ID=testlinux
VERSION=1.0
`
	osReleasePath := filepath.Join(etcDir, "os-release")
	if err := os.WriteFile(osReleasePath, []byte(osReleaseContent), 0644); err != nil {
		t.Fatalf("Failed to write os-release: %v", err)
	}

	result := ParseOSRelease(tmpDir)
	expected := "testlinux"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}
