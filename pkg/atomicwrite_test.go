package pkg

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAtomicWriteFile_WritesContentAndPerms verifies the happy path: content and
// permissions land exactly as requested.
func TestAtomicWriteFile_WritesContentAndPerms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "grub.cfg")
	want := []byte("set default=0\n")

	if err := atomicWriteFile(path, want, 0o644); err != nil {
		t.Fatalf("atomicWriteFile failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("content = %q, want %q", got, want)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("perm = %o, want 0644", info.Mode().Perm())
	}
}

// TestAtomicWriteFile_OverwritesExisting verifies an existing file is replaced
// with the new content.
func TestAtomicWriteFile_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "loader.conf")
	if err := os.WriteFile(path, []byte("old content that is longer"), 0o644); err != nil {
		t.Fatal(err)
	}

	want := []byte("new")
	if err := atomicWriteFile(path, want, 0o644); err != nil {
		t.Fatalf("atomicWriteFile failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("content = %q, want %q (truncation/overwrite failed)", got, want)
	}
}

// TestAtomicWriteFile_NoTempLitterOnSuccess verifies the temporary file used for
// the atomic rename is not left behind.
func TestAtomicWriteFile_NoTempLitterOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bootc.conf")

	if err := atomicWriteFile(path, []byte("title Snow\n"), 0o644); err != nil {
		t.Fatalf("atomicWriteFile failed: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected only the destination file, found %d entries: %v", len(entries), names)
	}
}

// TestAtomicWriteFile_PreservesExistingOnFailure is the core guarantee behind the
// fix: if the write cannot complete, the pre-existing file must NOT be left
// truncated or corrupt. A naive os.WriteFile truncates the destination before
// writing, so a mid-write crash destroys the previously-valid file (which, for
// grub.cfg holding both A/B entries, bricks the machine).
//
// We simulate an un-completable write by pointing at a path whose parent
// directory has been made read-only, so creating the temp file fails.
func TestAtomicWriteFile_PreservesExistingOnFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("read-only directory permissions are not enforced for root")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "grub.cfg")
	original := []byte("GOOD original config with both A/B entries\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	// Make the directory read-only so temp-file creation (and thus the write)
	// fails without touching the existing destination.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if err := atomicWriteFile(path, []byte("new content"), 0o644); err == nil {
		t.Fatal("expected atomicWriteFile to fail when the directory is read-only")
	}

	// Restore access to read the file back.
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Errorf("existing file was corrupted on failed write: got %q, want %q", got, original)
	}
}
