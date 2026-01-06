package pkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileLock_ExclusiveBlocksExclusive(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "test.lock")

	// Acquire first exclusive lock
	lock1, err := AcquireExclusive(lockPath)
	if err != nil {
		t.Fatalf("Failed to acquire first exclusive lock: %v", err)
	}
	defer func() { _ = lock1.Release() }()

	// Try to acquire second exclusive lock - should fail immediately
	lock2, err := AcquireExclusive(lockPath)
	if err == nil {
		_ = lock2.Release()
		t.Fatal("Expected second exclusive lock to fail, but it succeeded")
	}
	if err != ErrLockHeld {
		t.Fatalf("Expected ErrLockHeld, got: %v", err)
	}
}

func TestFileLock_SharedAllowsShared(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "test.lock")

	// Acquire first shared lock
	lock1, err := AcquireShared(lockPath)
	if err != nil {
		t.Fatalf("Failed to acquire first shared lock: %v", err)
	}
	defer func() { _ = lock1.Release() }()

	// Acquire second shared lock - should succeed
	lock2, err := AcquireShared(lockPath)
	if err != nil {
		t.Fatalf("Expected second shared lock to succeed, but got: %v", err)
	}
	defer func() { _ = lock2.Release() }()
}

func TestFileLock_ExclusiveBlocksShared(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "test.lock")

	// Acquire exclusive lock
	lock1, err := AcquireExclusive(lockPath)
	if err != nil {
		t.Fatalf("Failed to acquire exclusive lock: %v", err)
	}
	defer func() { _ = lock1.Release() }()

	// Try to acquire shared lock - should fail
	lock2, err := AcquireShared(lockPath)
	if err == nil {
		_ = lock2.Release()
		t.Fatal("Expected shared lock to fail when exclusive is held, but it succeeded")
	}
	if err != ErrLockHeld {
		t.Fatalf("Expected ErrLockHeld, got: %v", err)
	}
}

func TestFileLock_SharedBlocksExclusive(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "test.lock")

	// Acquire shared lock
	lock1, err := AcquireShared(lockPath)
	if err != nil {
		t.Fatalf("Failed to acquire shared lock: %v", err)
	}
	defer func() { _ = lock1.Release() }()

	// Try to acquire exclusive lock - should fail
	lock2, err := AcquireExclusive(lockPath)
	if err == nil {
		_ = lock2.Release()
		t.Fatal("Expected exclusive lock to fail when shared is held, but it succeeded")
	}
	if err != ErrLockHeld {
		t.Fatalf("Expected ErrLockHeld, got: %v", err)
	}
}

func TestFileLock_ReleaseAllowsReacquisition(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "test.lock")

	// Acquire and release lock
	lock1, err := AcquireExclusive(lockPath)
	if err != nil {
		t.Fatalf("Failed to acquire first lock: %v", err)
	}
	if err := lock1.Release(); err != nil {
		t.Fatalf("Failed to release first lock: %v", err)
	}

	// Should be able to acquire again
	lock2, err := AcquireExclusive(lockPath)
	if err != nil {
		t.Fatalf("Failed to acquire lock after release: %v", err)
	}
	defer func() { _ = lock2.Release() }()
}

func TestFileLock_ReleaseIsIdempotent(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "test.lock")

	lock, err := AcquireExclusive(lockPath)
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}

	// Release multiple times should not panic or error
	if err := lock.Release(); err != nil {
		t.Fatalf("First release failed: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Second release should not error: %v", err)
	}
}

func TestFileLock_NilReleaseIsSafe(t *testing.T) {
	var lock *FileLock
	// Should not panic
	if err := lock.Release(); err != nil {
		t.Fatalf("Release on nil lock should not error: %v", err)
	}
}

func TestFileLock_Path(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "test.lock")

	lock, err := AcquireExclusive(lockPath)
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}
	defer func() { _ = lock.Release() }()

	if lock.Path() != lockPath {
		t.Fatalf("Expected path %q, got %q", lockPath, lock.Path())
	}
}

func TestFileLock_PathOnNil(t *testing.T) {
	var lock *FileLock
	if lock.Path() != "" {
		t.Fatalf("Expected empty path for nil lock, got %q", lock.Path())
	}
}

func TestEnsureLockDir(t *testing.T) {
	// This test verifies that ensureLockDir doesn't error when the directory
	// already exists (idempotent behavior)
	err := ensureLockDir()
	if err != nil {
		// If we can't create /var/run/nbc, it might be a permission issue in test env
		// Check if it's because we don't have permission (unwrap the error)
		if os.IsPermission(err) || strings.Contains(err.Error(), "permission denied") {
			t.Skip("Skipping test: no permission to create /var/run/nbc")
		}
		t.Fatalf("ensureLockDir failed: %v", err)
	}

	// Call again to verify idempotency
	err = ensureLockDir()
	if err != nil {
		t.Fatalf("ensureLockDir should be idempotent: %v", err)
	}
}

func TestCacheLockPath(t *testing.T) {
	expected := "/var/run/nbc/cache.lock"
	if CacheLockPath() != expected {
		t.Fatalf("Expected %q, got %q", expected, CacheLockPath())
	}
}

func TestSystemLockPath(t *testing.T) {
	expected := "/var/run/nbc/system.lock"
	if SystemLockPath() != expected {
		t.Fatalf("Expected %q, got %q", expected, SystemLockPath())
	}
}

func TestAcquireCacheLock_UserFriendlyError(t *testing.T) {
	// Skip if we can't write to /var/run/nbc
	if err := ensureLockDir(); err != nil {
		t.Skip("Skipping test: cannot create lock directory")
	}

	// Acquire lock
	lock1, err := AcquireCacheLock()
	if err != nil {
		t.Fatalf("Failed to acquire first cache lock: %v", err)
	}
	defer func() { _ = lock1.Release() }()

	// Try to acquire again - should get user-friendly error
	_, err = AcquireCacheLock()
	if err == nil {
		t.Fatal("Expected error when lock is held")
	}
	expectedMsg := "another nbc process is currently operating on the cache"
	if err.Error() != expectedMsg {
		t.Fatalf("Expected error %q, got %q", expectedMsg, err.Error())
	}
}

func TestAcquireCacheLockShared_UserFriendlyError(t *testing.T) {
	// Skip if we can't write to /var/run/nbc
	if err := ensureLockDir(); err != nil {
		t.Skip("Skipping test: cannot create lock directory")
	}

	// Acquire exclusive lock
	lock1, err := AcquireCacheLock()
	if err != nil {
		t.Fatalf("Failed to acquire exclusive cache lock: %v", err)
	}
	defer func() { _ = lock1.Release() }()

	// Try to acquire shared - should get user-friendly error
	_, err = AcquireCacheLockShared()
	if err == nil {
		t.Fatal("Expected error when exclusive lock is held")
	}
	expectedMsg := "another nbc process is currently modifying the cache"
	if err.Error() != expectedMsg {
		t.Fatalf("Expected error %q, got %q", expectedMsg, err.Error())
	}
}

func TestAcquireSystemLock_UserFriendlyError(t *testing.T) {
	// Skip if we can't write to /var/run/nbc
	if err := ensureLockDir(); err != nil {
		t.Skip("Skipping test: cannot create lock directory")
	}

	// Acquire lock
	lock1, err := AcquireSystemLock()
	if err != nil {
		t.Fatalf("Failed to acquire first system lock: %v", err)
	}
	defer func() { _ = lock1.Release() }()

	// Try to acquire again - should get user-friendly error
	_, err = AcquireSystemLock()
	if err == nil {
		t.Fatal("Expected error when lock is held")
	}
	expectedMsg := "another nbc process is currently performing a system operation"
	if err.Error() != expectedMsg {
		t.Fatalf("Expected error %q, got %q", expectedMsg, err.Error())
	}
}
