package pkg

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCreateEphemeralKeyFile_UsesPreferredDir verifies the transient key file is
// created in a preferred (tmpfs) directory when one is available, so a LUKS
// passphrase is not written to a possibly disk-backed /tmp (#97).
func TestCreateEphemeralKeyFile_UsesPreferredDir(t *testing.T) {
	preferred := t.TempDir()
	orig := ephemeralKeyDirs
	t.Cleanup(func() { ephemeralKeyDirs = orig })
	ephemeralKeyDirs = []string{preferred}

	f, err := createEphemeralKeyFile("luks-key-*")
	if err != nil {
		t.Fatalf("createEphemeralKeyFile: %v", err)
	}
	path := f.Name()
	_ = f.Close()
	defer func() { _ = os.Remove(path) }()

	if got := filepath.Dir(path); got != preferred {
		t.Errorf("key file created in %q, want preferred dir %q", got, preferred)
	}
}

// TestCreateEphemeralKeyFile_FallsBackWhenNoneAvailable verifies we still create
// a key file (in the default temp dir) if no preferred dir exists.
func TestCreateEphemeralKeyFile_FallsBackWhenNoneAvailable(t *testing.T) {
	orig := ephemeralKeyDirs
	t.Cleanup(func() { ephemeralKeyDirs = orig })
	ephemeralKeyDirs = []string{filepath.Join(t.TempDir(), "does-not-exist")}

	f, err := createEphemeralKeyFile("luks-key-*")
	if err != nil {
		t.Fatalf("createEphemeralKeyFile fallback: %v", err)
	}
	path := f.Name()
	_ = f.Close()
	defer func() { _ = os.Remove(path) }()

	if _, err := os.Stat(path); err != nil {
		t.Errorf("fallback key file should exist: %v", err)
	}
}

// TestWriteEphemeralKeyFile_TightPermsAndCleanup verifies the secret is written
// with 0600 permissions and the cleanup removes the file.
func TestWriteEphemeralKeyFile_TightPermsAndCleanup(t *testing.T) {
	preferred := t.TempDir()
	orig := ephemeralKeyDirs
	t.Cleanup(func() { ephemeralKeyDirs = orig })
	ephemeralKeyDirs = []string{preferred}

	path, cleanup, err := writeEphemeralKeyFile("s3cr et with spaces")
	if err != nil {
		t.Fatalf("writeEphemeralKeyFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat key file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("key file perms = %o, want 0600", perm)
	}
	if data, _ := os.ReadFile(path); string(data) != "s3cr et with spaces" {
		t.Errorf("key file content = %q", data)
	}

	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("cleanup should remove the key file, stat err = %v", err)
	}
}
