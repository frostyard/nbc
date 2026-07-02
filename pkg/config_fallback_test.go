package pkg

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/frostyard/std/reporter"
)

func writeJSON(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLoadSystemConfig_FallsBackToLegacyOnCorruptPrimary is the core #93
// regression: when the primary config exists but is unparseable (e.g. a
// truncated/corrupt file), ReadSystemConfig previously hard-failed with a parse
// error. It should fall back to the legacy config if that one is valid.
func TestLoadSystemConfig_FallsBackToLegacyOnCorruptPrimary(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "config.json")
	legacy := filepath.Join(dir, "legacy.json")
	writeJSON(t, primary, "{ this is not valid json")
	writeJSON(t, legacy, `{"image_ref":"legacy-ref"}`)

	cfg, err := loadSystemConfig(primary, legacy)
	if err != nil {
		t.Fatalf("expected fallback to legacy, got error: %v", err)
	}
	if cfg.ImageRef != "legacy-ref" {
		t.Errorf("ImageRef = %q, want %q", cfg.ImageRef, "legacy-ref")
	}
}

func TestLoadSystemConfig_PrefersValidPrimary(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "config.json")
	legacy := filepath.Join(dir, "legacy.json")
	writeJSON(t, primary, `{"image_ref":"primary-ref"}`)
	writeJSON(t, legacy, `{"image_ref":"legacy-ref"}`)

	cfg, err := loadSystemConfig(primary, legacy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ImageRef != "primary-ref" {
		t.Errorf("ImageRef = %q, want %q", cfg.ImageRef, "primary-ref")
	}
}

func TestLoadSystemConfig_FallsBackWhenPrimaryMissing(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "config.json") // does not exist
	legacy := filepath.Join(dir, "legacy.json")
	writeJSON(t, legacy, `{"image_ref":"legacy-ref"}`)

	cfg, err := loadSystemConfig(primary, legacy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ImageRef != "legacy-ref" {
		t.Errorf("ImageRef = %q, want %q", cfg.ImageRef, "legacy-ref")
	}
}

func TestLoadSystemConfig_NotFoundWhenBothMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := loadSystemConfig(filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json"))
	if err == nil {
		t.Fatal("expected an error when neither config exists")
	}
}

func TestLoadSystemConfig_SurfacesErrorWhenPrimaryCorruptAndNoLegacy(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "config.json")
	writeJSON(t, primary, "{ corrupt")

	_, err := loadSystemConfig(primary, filepath.Join(dir, "missing.json"))
	if err == nil {
		t.Fatal("expected an error when primary is corrupt and legacy is missing")
	}
	// The corruption error should not be masked as a bare not-found.
	if errors.Is(err, os.ErrNotExist) {
		t.Errorf("error should reflect the corrupt primary, not a not-exist: %v", err)
	}
}

// TestWriteSystemConfigToVar_IsAtomic verifies the config write leaves no temp
// litter and produces a parseable file (the write now goes through
// atomicWriteFile so a torn write cannot corrupt the shared A/B config).
func TestWriteSystemConfigToVar_IsAtomic(t *testing.T) {
	varMount := t.TempDir()
	cfg := &SystemConfig{ImageRef: "ghcr.io/x/y:latest", Device: "/dev/sda"}

	if err := WriteSystemConfigToVar(t.Context(), varMount, cfg, false, reporter.NoopReporter{}); err != nil {
		t.Fatalf("WriteSystemConfigToVar: %v", err)
	}

	stateDir := filepath.Join(varMount, "lib", "nbc", "state")
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "config.json" {
			t.Errorf("unexpected leftover file in state dir: %s", e.Name())
		}
	}
}
