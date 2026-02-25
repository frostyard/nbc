package pkg

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// testProgress creates a Reporter for tests that discards output
func testProgress() Reporter {
	return NoopReporter{}
}

func TestConstants(t *testing.T) {
	t.Run("PristineEtcPath is correct", func(t *testing.T) {
		expected := "/var/lib/nbc/etc.pristine"
		if PristineEtcPath != expected {
			t.Errorf("PristineEtcPath = %q, want %q", PristineEtcPath, expected)
		}
	})

	t.Run("EtcOverlayPath is correct", func(t *testing.T) {
		expected := "/var/lib/nbc/etc-overlay"
		if EtcOverlayPath != expected {
			t.Errorf("EtcOverlayPath = %q, want %q", EtcOverlayPath, expected)
		}
	})
}

func TestSetupEtcOverlay(t *testing.T) {
	t.Run("dry run does not create directories", func(t *testing.T) {
		targetDir := t.TempDir()

		err := SetupEtcOverlay(context.Background(), targetDir, true, testProgress())
		if err != nil {
			t.Fatalf("SetupEtcOverlay dry run failed: %v", err)
		}

		// Verify no overlay directories were created
		overlayBase := filepath.Join(targetDir, "var", "lib", "nbc", "etc-overlay")
		if _, err := os.Stat(overlayBase); !os.IsNotExist(err) {
			t.Error("dry run should not create overlay directories")
		}
	})

	t.Run("creates overlay directories", func(t *testing.T) {
		targetDir := t.TempDir()

		// Create /etc with some content (required by SetupEtcOverlay)
		etcDir := filepath.Join(targetDir, "etc")
		if err := os.MkdirAll(etcDir, 0755); err != nil {
			t.Fatalf("failed to create etc dir: %v", err)
		}
		// Add required files
		for _, f := range []string{"passwd", "group", "os-release"} {
			if err := os.WriteFile(filepath.Join(etcDir, f), []byte("test"), 0644); err != nil {
				t.Fatalf("failed to create %s: %v", f, err)
			}
		}

		err := SetupEtcOverlay(context.Background(), targetDir, false, testProgress())
		if err != nil {
			t.Fatalf("SetupEtcOverlay failed: %v", err)
		}

		// Verify overlay directories were created
		upperDir := filepath.Join(targetDir, "var", "lib", "nbc", "etc-overlay", "upper")
		workDir := filepath.Join(targetDir, "var", "lib", "nbc", "etc-overlay", "work")

		if _, err := os.Stat(upperDir); err != nil {
			t.Errorf("upper directory not created: %v", err)
		}
		if _, err := os.Stat(workDir); err != nil {
			t.Errorf("work directory not created: %v", err)
		}
	})

	t.Run("fails if /etc does not exist", func(t *testing.T) {
		targetDir := t.TempDir()

		err := SetupEtcOverlay(context.Background(), targetDir, false, testProgress())
		if err == nil {
			t.Error("SetupEtcOverlay should fail when /etc does not exist")
		}
	})

	t.Run("fails if /etc is empty", func(t *testing.T) {
		targetDir := t.TempDir()

		// Create empty /etc
		etcDir := filepath.Join(targetDir, "etc")
		if err := os.MkdirAll(etcDir, 0755); err != nil {
			t.Fatalf("failed to create etc dir: %v", err)
		}

		err := SetupEtcOverlay(context.Background(), targetDir, false, testProgress())
		if err == nil {
			t.Error("SetupEtcOverlay should fail when /etc is empty")
		}
	})
}

func TestSetupEtcPersistence(t *testing.T) {
	t.Run("calls SetupEtcOverlay", func(t *testing.T) {
		targetDir := t.TempDir()

		// Create /etc with content
		etcDir := filepath.Join(targetDir, "etc")
		if err := os.MkdirAll(etcDir, 0755); err != nil {
			t.Fatalf("failed to create etc dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(etcDir, "test"), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		err := SetupEtcPersistence(context.Background(), targetDir, false, testProgress())
		if err != nil {
			t.Fatalf("SetupEtcPersistence failed: %v", err)
		}

		// Verify overlay directories were created (same as SetupEtcOverlay)
		upperDir := filepath.Join(targetDir, "var", "lib", "nbc", "etc-overlay", "upper")
		if _, err := os.Stat(upperDir); err != nil {
			t.Errorf("SetupEtcPersistence should create overlay directories: %v", err)
		}
	})
}

func TestInstallEtcMountUnit(t *testing.T) {
	t.Run("deprecated function works", func(t *testing.T) {
		targetDir := t.TempDir()

		// Create /etc with content
		etcDir := filepath.Join(targetDir, "etc")
		if err := os.MkdirAll(etcDir, 0755); err != nil {
			t.Fatalf("failed to create etc dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(etcDir, "test"), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		err := InstallEtcMountUnit(context.Background(), targetDir, false, testProgress())
		if err != nil {
			t.Fatalf("InstallEtcMountUnit failed: %v", err)
		}
	})
}

func TestMergeEtcFromActive(t *testing.T) {
	t.Run("dry run does nothing", func(t *testing.T) {
		targetDir := t.TempDir()

		err := MergeEtcFromActive(context.Background(), targetDir, "/dev/sda3", true, testProgress())
		if err != nil {
			t.Fatalf("MergeEtcFromActive dry run failed: %v", err)
		}
	})

	t.Run("creates overlay directories on new root", func(t *testing.T) {
		targetDir := t.TempDir()

		// Create /etc with content
		etcDir := filepath.Join(targetDir, "etc")
		if err := os.MkdirAll(etcDir, 0755); err != nil {
			t.Fatalf("failed to create etc dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(etcDir, "test"), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		err := MergeEtcFromActive(context.Background(), targetDir, "/dev/sda3", false, testProgress())
		if err != nil {
			t.Fatalf("MergeEtcFromActive failed: %v", err)
		}

		// Verify overlay directories exist
		upperDir := filepath.Join(targetDir, "var", "lib", "nbc", "etc-overlay", "upper")
		if _, err := os.Stat(upperDir); err != nil {
			t.Errorf("overlay directories should exist: %v", err)
		}
	})
}

func TestHashFile(t *testing.T) {
	t.Run("returns error for non-existent file", func(t *testing.T) {
		_, err := hashFile("/nonexistent/file")
		if err == nil {
			t.Error("hashFile should fail for non-existent file")
		}
	})

	t.Run("computes consistent hash", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "test.txt")
		content := "test content for hashing"
		if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		hash1, err := hashFile(tmpFile)
		if err != nil {
			t.Fatalf("hashFile failed: %v", err)
		}

		hash2, err := hashFile(tmpFile)
		if err != nil {
			t.Fatalf("hashFile failed on second call: %v", err)
		}

		if hash1 != hash2 {
			t.Errorf("hash should be consistent: %q != %q", hash1, hash2)
		}
	})

	t.Run("different content produces different hash", func(t *testing.T) {
		dir := t.TempDir()
		file1 := filepath.Join(dir, "file1.txt")
		file2 := filepath.Join(dir, "file2.txt")

		if err := os.WriteFile(file1, []byte("content 1"), 0644); err != nil {
			t.Fatalf("failed to create file1: %v", err)
		}
		if err := os.WriteFile(file2, []byte("content 2"), 0644); err != nil {
			t.Fatalf("failed to create file2: %v", err)
		}

		hash1, err := hashFile(file1)
		if err != nil {
			t.Fatalf("hashFile failed for file1: %v", err)
		}

		hash2, err := hashFile(file2)
		if err != nil {
			t.Fatalf("hashFile failed for file2: %v", err)
		}

		if hash1 == hash2 {
			t.Error("different content should produce different hashes")
		}
	})
}
