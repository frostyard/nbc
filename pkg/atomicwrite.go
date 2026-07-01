package pkg

import (
	"fmt"
	"os"
	"path/filepath"
)

// atomicWriteFile writes data to path atomically: it writes to a temporary file
// in the same directory, fsyncs it, then renames it over the destination and
// fsyncs the directory. A crash or power loss at any point leaves either the old
// file fully intact or the new file fully written -- never a truncated or
// partial file.
//
// This matters for boot-critical config files (grub.cfg, loader.conf, and
// systemd-boot entries) on the ESP: a plain os.WriteFile truncates the
// destination before writing, so an interruption mid-write can corrupt a config
// that was valid moments earlier and leave the machine unbootable.
//
// The temp file is created in the destination directory so the rename stays on
// the same filesystem (a cross-filesystem rename is not atomic and would fail).
// This works on FAT32/vfat ESPs, where rename is a directory-entry update.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()

	// Clean up the temp file on any error path before the rename succeeds.
	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("failed to write temp file for %s: %w", path, err)
	}
	// Best-effort permission set. These files live on the FAT32 ESP, where
	// permissions are governed by mount options and fchmod may legitimately
	// fail; on real filesystems this applies the requested mode. Either way the
	// file content is what matters for bootability, so a chmod failure must not
	// abort the atomic write.
	_ = tmp.Chmod(perm)
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp file for %s: %w", path, err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("failed to rename temp file into place for %s: %w", path, err)
	}
	committed = true

	// fsync the directory so the rename itself is durable. Best-effort: some
	// filesystems (e.g. certain vfat setups) may not support directory fsync,
	// and the rename is already the atomic-visibility guarantee.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}

	return nil
}
