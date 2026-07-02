package pkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/std/reporter"
)

func testCachedImageMetadata(imageRef, digest string) *CachedImageMetadata {
	return &CachedImageMetadata{
		ImageRef:            imageRef,
		ImageDigest:         digest,
		DownloadDate:        "2024-01-01T00:00:00Z",
		Architecture:        "amd64",
		Labels:              map[string]string{"version": "1.0"},
		OSReleasePrettyName: "Test OS",
		OSReleaseVersionID:  "1.0",
		OSReleaseID:         "testos",
		SizeBytes:           1024 * 1024,
	}
}

func makeStagingDir(t *testing.T, cache *ImageCache, metadata *CachedImageMetadata) string {
	t.Helper()

	stagingDir, err := os.MkdirTemp(cache.CacheDir, ".download-*")
	if err != nil {
		t.Fatalf("failed to create staging dir: %v", err)
	}

	if err := cache.writeMetadata(stagingDir, metadata); err != nil {
		t.Fatalf("failed to write staging metadata: %v", err)
	}

	return stagingDir
}

func assertMetadataEqual(t *testing.T, got, want *CachedImageMetadata) {
	t.Helper()

	if got.ImageRef != want.ImageRef {
		t.Errorf("ImageRef = %q, want %q", got.ImageRef, want.ImageRef)
	}
	if got.ImageDigest != want.ImageDigest {
		t.Errorf("ImageDigest = %q, want %q", got.ImageDigest, want.ImageDigest)
	}
	if got.Architecture != want.Architecture {
		t.Errorf("Architecture = %q, want %q", got.Architecture, want.Architecture)
	}
	if got.SizeBytes != want.SizeBytes {
		t.Errorf("SizeBytes = %d, want %d", got.SizeBytes, want.SizeBytes)
	}
}

func skipIfNoCacheLockPermission(t *testing.T) {
	t.Helper()

	if err := ensureLockDir(); err != nil {
		if os.IsPermission(err) || strings.Contains(err.Error(), "permission denied") {
			t.Skip("Skipping test: no permission to create /var/run/nbc")
		}
		t.Fatalf("ensureLockDir failed: %v", err)
	}
}

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

func TestCacheCommitDownload_AtomicMove(t *testing.T) {
	skipIfNoCacheLockPermission(t)

	tmpDir := t.TempDir()
	cache := NewImageCache(tmpDir)
	metadata := testCachedImageMetadata("quay.io/test/image:v1", "sha256:abc123")
	stagingDir := makeStagingDir(t, cache, metadata)

	sharedLock, err := AcquireCacheLockShared()
	if err != nil {
		t.Fatalf("AcquireCacheLockShared() during staging failed: %v", err)
	}
	if err := sharedLock.Release(); err != nil {
		t.Fatalf("failed to release shared cache lock: %v", err)
	}

	got, committed, err := cache.commitDownload(stagingDir, metadata.ImageDigest, metadata, reporter.NoopReporter{})
	if err != nil {
		t.Fatalf("commitDownload() error = %v", err)
	}
	if !committed {
		t.Fatal("commitDownload() committed = false, want true")
	}
	assertMetadataEqual(t, got, metadata)

	finalDir := filepath.Join(tmpDir, "sha256-abc123")
	if _, err := os.Stat(finalDir); err != nil {
		t.Fatalf("final image dir does not exist: %v", err)
	}
	if _, err := os.Stat(stagingDir); !os.IsNotExist(err) {
		t.Fatalf("staging dir should have been renamed away, stat err = %v", err)
	}
}

func TestCacheCommitDownload_DedupWhenAlreadyCached(t *testing.T) {
	skipIfNoCacheLockPermission(t)

	tmpDir := t.TempDir()
	cache := NewImageCache(tmpDir)
	existing := testCachedImageMetadata("quay.io/test/existing:v1", "sha256:abc123")
	staged := testCachedImageMetadata("quay.io/test/staged:v1", "sha256:abc123")

	finalDir := filepath.Join(tmpDir, "sha256-abc123")
	if err := os.MkdirAll(finalDir, 0755); err != nil {
		t.Fatalf("failed to create final dir: %v", err)
	}
	if err := cache.writeMetadata(finalDir, existing); err != nil {
		t.Fatalf("failed to write existing metadata: %v", err)
	}
	stagingDir := makeStagingDir(t, cache, staged)

	got, committed, err := cache.commitDownload(stagingDir, staged.ImageDigest, staged, reporter.NoopReporter{})
	if err != nil {
		t.Fatalf("commitDownload() error = %v", err)
	}
	if committed {
		t.Fatal("commitDownload() committed = true, want false")
	}
	assertMetadataEqual(t, got, existing)
	if _, err := os.Stat(stagingDir); err != nil {
		t.Fatalf("staging dir should be left for caller cleanup: %v", err)
	}
}

func TestCacheCommitDownload_ReplacesIncompleteEntry(t *testing.T) {
	skipIfNoCacheLockPermission(t)

	tmpDir := t.TempDir()
	cache := NewImageCache(tmpDir)
	metadata := testCachedImageMetadata("quay.io/test/image:v1", "sha256:abc123")

	finalDir := filepath.Join(tmpDir, "sha256-abc123")
	if err := os.MkdirAll(finalDir, 0755); err != nil {
		t.Fatalf("failed to create incomplete final dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(finalDir, "partial"), []byte("incomplete"), 0644); err != nil {
		t.Fatalf("failed to write incomplete marker: %v", err)
	}
	stagingDir := makeStagingDir(t, cache, metadata)

	got, committed, err := cache.commitDownload(stagingDir, metadata.ImageDigest, metadata, reporter.NoopReporter{})
	if err != nil {
		t.Fatalf("commitDownload() error = %v", err)
	}
	if !committed {
		t.Fatal("commitDownload() committed = false, want true")
	}
	assertMetadataEqual(t, got, metadata)

	if _, err := os.Stat(filepath.Join(finalDir, "partial")); !os.IsNotExist(err) {
		t.Fatalf("incomplete entry should have been replaced, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(finalDir, MetadataFileName)); err != nil {
		t.Fatalf("replacement metadata missing: %v", err)
	}
}

func TestCacheListUnlocked_SkipsStagingDirs(t *testing.T) {
	skipIfNoCacheLockPermission(t)

	tmpDir := t.TempDir()
	cache := NewImageCache(tmpDir)

	valid := testCachedImageMetadata("quay.io/test/image:v1", "sha256:abc123")
	validDir := filepath.Join(tmpDir, "sha256-abc123")
	if err := os.MkdirAll(validDir, 0755); err != nil {
		t.Fatalf("failed to create valid image dir: %v", err)
	}
	if err := cache.writeMetadata(validDir, valid); err != nil {
		t.Fatalf("failed to write valid metadata: %v", err)
	}

	stagingDir := filepath.Join(tmpDir, ".download-abc123")
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		t.Fatalf("failed to create staging dir: %v", err)
	}
	if err := cache.writeMetadata(stagingDir, testCachedImageMetadata("quay.io/test/staged:v1", "sha256:def456")); err != nil {
		t.Fatalf("failed to write staging metadata: %v", err)
	}

	images, err := cache.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("List() returned %d images, want 1", len(images))
	}
	if images[0].ImageDigest != valid.ImageDigest {
		t.Errorf("List()[0].ImageDigest = %q, want %q", images[0].ImageDigest, valid.ImageDigest)
	}
}

func TestCacheGetImage_IgnoresStagingDirs(t *testing.T) {
	skipIfNoCacheLockPermission(t)

	tmpDir := t.TempDir()
	cache := NewImageCache(tmpDir)

	stagingDir := filepath.Join(tmpDir, ".download-sha256-abc123")
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		t.Fatalf("failed to create staging dir: %v", err)
	}
	staged := testCachedImageMetadata("quay.io/test/staged:v1", "sha256:abc123")
	if err := cache.writeMetadata(stagingDir, staged); err != nil {
		t.Fatalf("failed to write staging metadata: %v", err)
	}

	_, _, err := cache.GetImage(staged.ImageRef)
	if err == nil {
		t.Fatal("GetImage() should ignore staging dir and report not found")
	}
	expected := "image not found in cache: " + staged.ImageRef
	if err.Error() != expected {
		t.Fatalf("GetImage() error = %q, want %q", err.Error(), expected)
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
	if err := cache.Remove(t.Context(), "sha256:abc123def456", reporter.NoopReporter{}); err != nil {
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
	if err := cache.Remove(t.Context(), "sha256:abc123", reporter.NoopReporter{}); err != nil {
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
	err := cache.Remove(t.Context(), "sha256:abc123", reporter.NoopReporter{})
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
	if err := cache.Clear(t.Context(), reporter.NoopReporter{}); err != nil {
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
