package pkg

import (
	"fmt"
	"os"
)

// ephemeralKeyDirs lists preferred directories for transient secret files, in
// priority order. These are tmpfs (memory-backed) on Linux, so a secret written
// there never touches persistent storage. It is a variable so tests can
// override it.
var ephemeralKeyDirs = []string{"/dev/shm", "/run"}

// createEphemeralKeyFile creates a 0600 temp file for a transient secret in the
// first available preferred (tmpfs) directory, falling back to the default temp
// dir if none is usable. os.CreateTemp already creates the file with 0600.
func createEphemeralKeyFile(pattern string) (*os.File, error) {
	for _, dir := range ephemeralKeyDirs {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			continue
		}
		if f, err := os.CreateTemp(dir, pattern); err == nil {
			return f, nil
		}
	}
	return os.CreateTemp("", pattern)
}

// writeEphemeralKeyFile writes secret to a 0600 temp file in a tmpfs-backed
// directory when available, and returns the path plus a cleanup func that
// overwrites (best-effort shred) and removes the file. Keeping the secret off
// disk is the primary protection; the overwrite is defense-in-depth.
func writeEphemeralKeyFile(secret string) (string, func(), error) {
	f, err := createEphemeralKeyFile("luks-key-*")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temporary key file: %w", err)
	}
	path := f.Name()
	cleanup := func() { shredFile(path) }

	if _, err := f.WriteString(secret); err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, fmt.Errorf("failed to write to temporary key file: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to close temporary key file: %w", err)
	}
	return path, cleanup, nil
}

// shredFile overwrites a file's contents with zeros before removing it. On tmpfs
// this zeroes the backing RAM pages before they are freed; on other filesystems
// it is best-effort (copy-on-write / wear-leveling may relocate blocks).
func shredFile(path string) {
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		if f, err := os.OpenFile(path, os.O_WRONLY, 0o600); err == nil {
			zeros := make([]byte, info.Size())
			_, _ = f.Write(zeros)
			_ = f.Sync()
			_ = f.Close()
		}
	}
	_ = os.Remove(path)
}
