package pkg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewImageCache(t *testing.T) {
	cache := NewImageCache("/test/path")
	if cache.CacheDir != "/test/path" {
		t.Errorf("expected /test/path, got %s", cache.CacheDir)
	}
}

func TestNewStagedInstallCache(t *testing.T) {
	cache := NewStagedInstallCache()
	if cache.CacheDir != StagedInstallDir {
		t.Errorf("expected %s, got %s", StagedInstallDir, cache.CacheDir)
	}
}

func TestNewStagedUpdateCache(t *testing.T) {
	cache := NewStagedUpdateCache()
	if cache.CacheDir != StagedUpdateDir {
		t.Errorf("expected %s, got %s", StagedUpdateDir, cache.CacheDir)
	}
}

func TestDigestToDir(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"sha256:abc123", "sha256-abc123"},
		{"sha512:def456", "sha512-def456"},
		{"abc123", "abc123"},
	}

	for _, tt := range tests {
		result := digestToDir(tt.input)
		if result != tt.expected {
			t.Errorf("digestToDir(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestDirToDigest(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"sha256-abc123", "sha256:abc123"},
		{"sha512-def456", "sha512:def456"},
		{"abc123", "abc123"},
	}

	for _, tt := range tests {
		result := dirToDigest(tt.input)
		if result != tt.expected {
			t.Errorf("dirToDigest(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestImageCache_List_EmptyCache(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	cache := NewImageCache(tmpDir)

	images, err := cache.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(images) != 0 {
		t.Errorf("List() returned %d images, want 0", len(images))
	}
}

func TestImageCache_List_NonExistentDirectory(t *testing.T) {
	cache := NewImageCache("/nonexistent/path/that/does/not/exist")

	images, err := cache.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(images) != 0 {
		t.Errorf("List() returned %d images, want 0", len(images))
	}
}

func TestImageCache_IsCached(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewImageCache(tmpDir)

	// Create a fake cached image directory
	imageDir := filepath.Join(tmpDir, "sha256-abc123")
	if err := os.MkdirAll(imageDir, 0755); err != nil {
		t.Fatalf("failed to create image dir: %v", err)
	}

	// Should find the cached image
	cached, err := cache.IsCached("sha256:abc123")
	if err != nil {
		t.Fatalf("IsCached() error = %v", err)
	}
	if !cached {
		t.Error("IsCached() = false, want true")
	}

	// Should not find non-existent image
	cached, err = cache.IsCached("sha256:nonexistent")
	if err != nil {
		t.Fatalf("IsCached() error = %v", err)
	}
	if cached {
		t.Error("IsCached() = true, want false")
	}
}

func TestImageCache_Remove(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewImageCache(tmpDir)

	// Create a fake cached image directory
	imageDir := filepath.Join(tmpDir, "sha256-abc123def456")
	if err := os.MkdirAll(imageDir, 0755); err != nil {
		t.Fatalf("failed to create image dir: %v", err)
	}

	// Remove by full digest
	if err := cache.Remove("sha256:abc123def456", NoopReporter{}); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	// Verify it's gone
	if _, err := os.Stat(imageDir); !os.IsNotExist(err) {
		t.Error("image directory should not exist after removal")
	}
}

func TestImageCache_Remove_ByPrefix(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewImageCache(tmpDir)

	// Create a fake cached image directory
	imageDir := filepath.Join(tmpDir, "sha256-abc123def456")
	if err := os.MkdirAll(imageDir, 0755); err != nil {
		t.Fatalf("failed to create image dir: %v", err)
	}

	// Remove by prefix
	if err := cache.Remove("sha256:abc123", NoopReporter{}); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	// Verify it's gone
	if _, err := os.Stat(imageDir); !os.IsNotExist(err) {
		t.Error("image directory should not exist after removal")
	}
}

func TestImageCache_Remove_AmbiguousPrefix(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewImageCache(tmpDir)

	// Create two directories with same prefix
	if err := os.MkdirAll(filepath.Join(tmpDir, "sha256-abc123def"), 0755); err != nil {
		t.Fatalf("failed to create image dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "sha256-abc123ghi"), 0755); err != nil {
		t.Fatalf("failed to create image dir: %v", err)
	}

	// Remove should fail with ambiguous prefix
	err := cache.Remove("sha256:abc123", NoopReporter{})
	if err == nil {
		t.Error("Remove() should fail with ambiguous prefix")
	}
}

func TestImageCache_Clear(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewImageCache(tmpDir)

	// Create some directories
	if err := os.MkdirAll(filepath.Join(tmpDir, "sha256-abc123"), 0755); err != nil {
		t.Fatalf("failed to create image dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "sha256-def456"), 0755); err != nil {
		t.Fatalf("failed to create image dir: %v", err)
	}

	// Clear the cache
	if err := cache.Clear(NoopReporter{}); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}

	// Verify directories are gone
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("cache should be empty after Clear(), found %d entries", len(entries))
	}
}

func TestImageCache_GetSingle_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewImageCache(tmpDir)

	metadata, err := cache.GetSingle()
	if err != nil {
		t.Fatalf("GetSingle() error = %v", err)
	}
	if metadata != nil {
		t.Error("GetSingle() should return nil for empty cache")
	}
}

func TestCachedImageMetadata_JSON(t *testing.T) {
	metadata := &CachedImageMetadata{
		ImageRef:            "quay.io/test/image:v1",
		ImageDigest:         "sha256:abc123",
		DownloadDate:        "2024-01-01T00:00:00Z",
		Architecture:        "amd64",
		Labels:              map[string]string{"version": "1.0"},
		OSReleasePrettyName: "Test OS",
		OSReleaseVersionID:  "1.0",
		OSReleaseID:         "testos",
		SizeBytes:           1024 * 1024,
	}

	if metadata.ImageRef != "quay.io/test/image:v1" {
		t.Errorf("unexpected ImageRef: %s", metadata.ImageRef)
	}
	if metadata.SizeBytes != 1024*1024 {
		t.Errorf("unexpected SizeBytes: %d", metadata.SizeBytes)
	}
}
