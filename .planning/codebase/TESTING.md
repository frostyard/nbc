# Testing Patterns

**Analysis Date:** 2026-01-26

## Test Framework

**Runner:**
- Go's built-in testing package (`testing`)
- No external test runner

**Assertion Library:**
- Standard library `testing.T` methods
- Manual comparisons with `t.Error()`, `t.Errorf()`, `t.Fatal()`, `t.Fatalf()`
- No assertion libraries (testify, gomega, etc.)

**Run Commands:**
```bash
make test           # Run all tests (unit then integration)
make test-unit      # Run unit tests only (no root required)
make test-integration  # Run integration tests (requires root)
make test-coverage  # Run tests with coverage report
make test-install   # Run installation tests (requires root)
make test-update    # Run update tests (requires root)
make test-incus     # Run Incus VM integration tests
make test-all       # Run unit + integration tests
```

**Direct Go Commands:**
```bash
go test -v ./pkg/... -run "^Test[^I]" -skip "Integration"  # Unit tests
go test -v ./pkg/... -run "^TestIntegration_" -timeout 10m  # Integration tests
go test -v ./pkg/... -coverprofile=coverage.out -covermode=atomic  # Coverage
```

## Test File Organization

**Location:**
- Co-located with source files in `pkg/` directory
- Test utilities in `pkg/testutil/` subdirectory

**Naming:**
- Pattern: `<source>_test.go`
- Examples: `config_test.go`, `disk_test.go`, `install_test.go`, `integration_test.go`

**Structure:**
```
pkg/
├── config.go
├── config_test.go
├── disk.go
├── disk_test.go
├── install.go
├── install_test.go
├── integration_test.go     # Cross-cutting integration tests
├── testutil/
│   └── disk.go             # Test helpers for disk operations
└── types/
    └── types.go            # No tests (pure data types)
```

## Test Structure

**Unit Test Pattern:**
```go
func TestFormatSize(t *testing.T) {
    tests := []struct {
        name string
        size uint64
        want string
    }{
        {
            name: "bytes",
            size: 512,
            want: "512 B",
        },
        {
            name: "kilobytes",
            size: 1024,
            want: "1.0 KB",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := FormatSize(tt.size)
            if got != tt.want {
                t.Errorf("FormatSize() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

**Subtests for Behavior Grouping:**
```go
func TestInstallConfig_Validate(t *testing.T) {
    tests := []struct {
        name    string
        config  InstallConfig
        wantErr string
    }{
        {
            name: "valid config with image and device",
            config: InstallConfig{
                ImageRef: "quay.io/example/image:latest",
                Device:   "/dev/sda",
            },
            wantErr: "",
        },
        {
            name:    "missing image and local image",
            config:  InstallConfig{Device: "/dev/sda"},
            wantErr: "either ImageRef or LocalImage is required",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := tt.config.Validate()
            if tt.wantErr == "" {
                if err != nil {
                    t.Errorf("Validate() unexpected error: %v", err)
                }
            } else {
                if err == nil {
                    t.Errorf("Validate() expected error containing %q, got nil", tt.wantErr)
                } else if !strings.Contains(err.Error(), tt.wantErr) {
                    t.Errorf("Validate() error = %q, want error containing %q", err.Error(), tt.wantErr)
                }
            }
        })
    }
}
```

**Integration Test Naming:**
```go
// Integration tests are prefixed with TestIntegration_
func TestIntegration_PartitionAndFormat(t *testing.T) {
    testutil.RequireRoot(t)
    testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4", "blkid")
    // ...
}
```

## Mocking

**Framework:** No mocking framework; manual test doubles

**Patterns:**

**Test Disk Creation (using loopback):**
```go
func TestIntegration_PartitionAndFormat(t *testing.T) {
    testutil.RequireRoot(t)
    testutil.RequireTools(t, "losetup", "sgdisk")

    disk, err := testutil.CreateTestDisk(t, 50)  // 50GB sparse image
    if err != nil {
        t.Fatalf("Failed to create test disk: %v", err)
    }
    // disk.GetDevice() returns /dev/loopX
    // Cleanup is automatic via t.Cleanup()
}
```

**Mock Container Image:**
```go
func TestIntegration_Installer_Install(t *testing.T) {
    imageName := "localhost/nbc-test-installer:latest"
    if err := testutil.CreateMockContainer(t, imageName); err != nil {
        t.Fatalf("Failed to create mock container: %v", err)
    }
    // Container is auto-removed via t.Cleanup()
}
```

**Callback Tracking:**
```go
var steps []string
var messages []string
installer.SetCallbacks(&InstallCallbacks{
    OnStep: func(step, total int, name string) {
        steps = append(steps, name)
    },
    OnMessage: func(msg string) {
        messages = append(messages, msg)
    },
})

result, err := installer.Install(context.Background())

if len(steps) == 0 {
    t.Error("OnStep callback was never invoked")
}
```

**What to Mock:**
- File system operations (use `t.TempDir()`)
- Block devices (use loopback via `testutil.CreateTestDisk`)
- Container images (use `testutil.CreateMockContainer`)

**What NOT to Mock:**
- Go standard library functions
- Simple utility functions
- External command execution in integration tests (test the real thing)

## Fixtures and Factories

**Test Data (inline):**
```go
testFiles := map[string]string{
    "passwd":      "root:x:0:0:root:/root:/bin/bash\n",
    "group":       "root:x:0:\n",
    "os-release":  "NAME=TestOS\nVERSION=1.0\n",
}

for name, content := range testFiles {
    path := filepath.Join(etcDir, name)
    if err := os.WriteFile(path, []byte(content), 0644); err != nil {
        t.Fatalf("Failed to create %s: %v", name, err)
    }
}
```

**Test Disk Factory (`pkg/testutil/disk.go`):**
```go
type TestDisk struct {
    ImagePath  string
    LoopDevice string
    Size       int64
    t          *testing.T
}

func CreateTestDisk(t *testing.T, sizeGB int) (*TestDisk, error) {
    t.Helper()
    // Creates sparse file, attaches to loop device
    // Registers t.Cleanup() for automatic teardown
    return disk, nil
}

func (d *TestDisk) GetDevice() string {
    return d.LoopDevice
}
```

**Mock Container Factory:**
```go
func CreateMockContainer(t *testing.T, imageName string) error {
    t.Helper()
    // Creates minimal rootfs with:
    // - /etc/passwd, /etc/group, /etc/os-release
    // - /usr/lib/modules/6.6.0-test/vmlinuz, initramfs.img
    // - /usr/lib/systemd/boot/efi/systemd-bootx64.efi
    // - 105MB padding for VerifyExtraction check
    // Builds with podman, registers cleanup
}
```

**Location:**
- Fixtures created inline in test functions
- Factory functions in `pkg/testutil/disk.go`

## Coverage

**Requirements:** None enforced, but coverage available

**Generate Coverage:**
```bash
make test-coverage
# Or directly:
go test -v ./pkg/... -coverprofile=coverage.out -covermode=atomic
go tool cover -html=coverage.out -o coverage.html
```

**View Coverage:**
```bash
open coverage.html  # or browser of choice
```

## Test Types

**Unit Tests:**
- Pure logic testing without external dependencies
- No root required
- Fast execution
- Run with: `make test-unit`
- Pattern: `TestXxx` (not prefixed with `Integration_`)

```go
func TestFormatSize(t *testing.T) { /* ... */ }
func TestInstallConfig_Validate(t *testing.T) { /* ... */ }
func TestNewInstaller(t *testing.T) { /* ... */ }
```

**Integration Tests:**
- Require root privileges (loopback devices, partitioning)
- Require external tools (sgdisk, mkfs.*, cryptsetup)
- Use real loopback devices and filesystems
- Longer execution time
- Run with: `make test-integration`
- Pattern: `TestIntegration_Xxx`

```go
func TestIntegration_PartitionAndFormat(t *testing.T) {
    testutil.RequireRoot(t)
    testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4")
    // ...
}
```

**VM Integration Tests (Incus):**
- Full end-to-end testing in VMs
- Require Incus container/VM manager
- Test actual boot and system behavior
- Run with: `make test-incus`, `make test-incus-quick`, etc.
- Scripts: `test_incus.sh`, `test_incus_quick.sh`, `test_incus_encryption.sh`

## Common Patterns

**Skip Conditions:**
```go
// Skip if not root
func RequireRoot(t *testing.T) {
    t.Helper()
    if os.Geteuid() != 0 {
        t.Skip("Test requires root privileges (sudo)")
    }
}

// Skip if tools missing
func RequireTools(t *testing.T, tools ...string) {
    t.Helper()
    for _, tool := range tools {
        if _, err := exec.LookPath(tool); err != nil {
            t.Skipf("Required tool not found: %s", tool)
        }
    }
}

// Skip if device doesn't exist
if _, err := os.Stat("/dev/disk/by-id"); os.IsNotExist(err) {
    t.Skip("Skipping test: /dev/disk/by-id does not exist")
}
```

**Temp Directory Usage:**
```go
func TestWriteSystemConfigToVar(t *testing.T) {
    t.Run("creates config file", func(t *testing.T) {
        varMountPoint := t.TempDir()  // Auto-cleaned up
        config := &SystemConfig{
            ImageRef: "ghcr.io/test/image:latest",
        }
        err := WriteSystemConfigToVar(varMountPoint, config, false, NewProgressReporter(false, 1))
        if err != nil {
            t.Fatalf("WriteSystemConfigToVar failed: %v", err)
        }
        // Verify file exists...
    })
}
```

**Cleanup with Defers:**
```go
func TestIntegration_MountUnmount(t *testing.T) {
    // Mount partitions...
    defer func() {
        _ = UnmountPartitions(context.Background(), mountPoint, false, progress)
    }()
    // Test code...
}

// Or with t.Cleanup:
t.Cleanup(func() {
    testutil.CleanupMounts(t, verifyMount)
})
```

**Error Checking Pattern:**
```go
if err != nil {
    t.Fatalf("operation failed: %v", err)  // Fatal for setup errors
}

if got != want {
    t.Errorf("result = %v, want %v", got, want)  // Error for assertion failures
}
```

**Async Testing:**
```go
// Context with cancellation
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

result, err := installer.Install(ctx)
```

**Error Testing:**
```go
t.Run("invalid JSON", func(t *testing.T) {
    tmpDir := t.TempDir()
    configPath := filepath.Join(tmpDir, "config.json")

    if err := os.WriteFile(configPath, []byte("{invalid json}"), 0644); err != nil {
        t.Fatalf("failed to write test config: %v", err)
    }

    err := verifyConfigFile(configPath)
    if err == nil {
        t.Error("verifyConfigFile should fail for invalid JSON")
    }
})
```

**Nil Safety Testing:**
```go
t.Run("nil callbacks does not panic", func(t *testing.T) {
    adapter := newCallbackProgressAdapter(nil, 6)
    // These should not panic
    adapter.Step(1, "Test")
    adapter.Message("Test")
    adapter.Warning("Test")
})
```

## Test Helpers

**Location:** `pkg/testutil/disk.go`

**Available Functions:**
```go
// CreateTestDisk creates a sparse disk image attached to loop device
func CreateTestDisk(t *testing.T, sizeGB int) (*TestDisk, error)

// RequireRoot skips test if not running as root
func RequireRoot(t *testing.T)

// RequireTools skips test if any tool is missing
func RequireTools(t *testing.T, tools ...string)

// CreateMockContainer builds a minimal container image for testing
func CreateMockContainer(t *testing.T, imageName string) error

// WaitForDevice waits for device to appear (after partitioning)
func WaitForDevice(device string) error

// CleanupMounts unmounts any mounts under a directory
func CleanupMounts(t *testing.T, mountPoint string)
```

**Usage Example:**
```go
func TestIntegration_PartitionAndFormat(t *testing.T) {
    testutil.RequireRoot(t)
    testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4", "blkid")

    disk, err := testutil.CreateTestDisk(t, 50)
    if err != nil {
        t.Fatalf("Failed to create test disk: %v", err)
    }

    scheme, err := CreatePartitions(context.Background(), disk.GetDevice(), false, NewProgressReporter(false, 1))
    if err != nil {
        t.Fatalf("CreatePartitions failed: %v", err)
    }

    _ = testutil.WaitForDevice(disk.GetDevice())
    // Continue with formatting...
}
```

## Dry-Run Testing

**Pattern for testing dry-run mode:**
```go
func TestIntegration_Installer_DryRun(t *testing.T) {
    testutil.RequireRoot(t)

    cfg := &InstallConfig{
        ImageRef:   imageName,
        Device:     disk.GetDevice(),
        DryRun:     true,  // Enable dry-run
    }

    installer, _ := NewInstaller(cfg)
    result, err := installer.Install(context.Background())
    if err != nil {
        t.Fatalf("Dry-run Install() failed: %v", err)
    }

    // Verify nothing was actually created
    _, err = DetectExistingPartitionScheme(disk.GetDevice())
    if err == nil {
        t.Error("Dry-run created partitions (should not have)")
    }
}
```

## Running Tests

**Recommended Workflow:**
```bash
# Quick unit tests during development
make test-unit

# Full test suite before commit (requires root)
sudo make test

# Integration tests only
sudo make test-integration

# Specific test
go test -v ./pkg/... -run TestInstallConfig_Validate

# With coverage
sudo make test-coverage
```

---

*Testing analysis: 2026-01-26*
