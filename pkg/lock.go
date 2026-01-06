package pkg

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const (
	// LockDir is the directory where lock files are stored
	LockDir = "/var/run/nbc"
	// CacheLockFile is the lock file for cache operations
	CacheLockFile = "cache.lock"
	// SystemLockFile is the lock file for system operations (install/update)
	SystemLockFile = "system.lock"
)

var (
	// ErrLockHeld is returned when a lock cannot be acquired because another process holds it
	ErrLockHeld = errors.New("lock held by another process")
)

// FileLock represents a file-based lock using flock
type FileLock struct {
	file *os.File
	path string
}

// CacheLockPath returns the full path to the cache lock file
func CacheLockPath() string {
	return filepath.Join(LockDir, CacheLockFile)
}

// SystemLockPath returns the full path to the system lock file
func SystemLockPath() string {
	return filepath.Join(LockDir, SystemLockFile)
}

// ensureLockDir creates the lock directory if it doesn't exist
func ensureLockDir() error {
	if err := os.MkdirAll(LockDir, 0755); err != nil {
		return fmt.Errorf("failed to create lock directory %s: %w", LockDir, err)
	}
	return nil
}

// AcquireExclusive acquires an exclusive (write) lock on the given path.
// Returns ErrLockHeld if the lock is already held by another process.
// The lock is automatically released when Release() is called or the process exits.
func AcquireExclusive(lockPath string) (*FileLock, error) {
	return acquireLock(lockPath, syscall.LOCK_EX, false)
}

// AcquireShared acquires a shared (read) lock on the given path.
// Multiple processes can hold shared locks simultaneously.
// Returns ErrLockHeld if an exclusive lock is held by another process.
// The lock is automatically released when Release() is called or the process exits.
func AcquireShared(lockPath string) (*FileLock, error) {
	return acquireLock(lockPath, syscall.LOCK_SH, false)
}

// acquireLock is the internal implementation for acquiring locks
func acquireLock(lockPath string, lockType int, ensureDir bool) (*FileLock, error) {
	// Ensure lock directory exists if requested
	if ensureDir {
		if err := ensureLockDir(); err != nil {
			return nil, err
		}
	} else {
		// Ensure parent directory exists
		dir := filepath.Dir(lockPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create lock directory %s: %w", dir, err)
		}
	}

	// Open or create the lock file
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open lock file %s: %w", lockPath, err)
	}

	// Try to acquire the lock (non-blocking)
	err = syscall.Flock(int(file.Fd()), lockType|syscall.LOCK_NB)
	if err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLockHeld
		}
		return nil, fmt.Errorf("failed to acquire lock on %s: %w", lockPath, err)
	}

	return &FileLock{
		file: file,
		path: lockPath,
	}, nil
}

// Release releases the lock and closes the underlying file.
// It is safe to call Release multiple times.
func (l *FileLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}

	// Closing the file automatically releases the flock
	err := l.file.Close()
	l.file = nil
	return err
}

// Path returns the path of the lock file
func (l *FileLock) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// acquireWithEnsureDir acquires a lock and ensures the standard lock directory exists
func acquireWithEnsureDir(lockPath string, lockType int) (*FileLock, error) {
	return acquireLock(lockPath, lockType, true)
}

// AcquireCacheLock acquires an exclusive lock for cache write operations.
// Returns a user-friendly error if the lock is already held.
func AcquireCacheLock() (*FileLock, error) {
	lock, err := acquireWithEnsureDir(CacheLockPath(), syscall.LOCK_EX)
	if err != nil {
		if errors.Is(err, ErrLockHeld) {
			return nil, fmt.Errorf("another nbc process is currently operating on the cache")
		}
		return nil, err
	}
	return lock, nil
}

// AcquireCacheLockShared acquires a shared lock for cache read operations.
// Returns a user-friendly error if an exclusive lock is held.
func AcquireCacheLockShared() (*FileLock, error) {
	lock, err := acquireWithEnsureDir(CacheLockPath(), syscall.LOCK_SH)
	if err != nil {
		if errors.Is(err, ErrLockHeld) {
			return nil, fmt.Errorf("another nbc process is currently modifying the cache")
		}
		return nil, err
	}
	return lock, nil
}

// AcquireSystemLock acquires an exclusive lock for system operations (install/update).
// Returns a user-friendly error if the lock is already held.
func AcquireSystemLock() (*FileLock, error) {
	lock, err := acquireWithEnsureDir(SystemLockPath(), syscall.LOCK_EX)
	if err != nil {
		if errors.Is(err, ErrLockHeld) {
			return nil, fmt.Errorf("another nbc process is currently performing a system operation")
		}
		return nil, err
	}
	return lock, nil
}
