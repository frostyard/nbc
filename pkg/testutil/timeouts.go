package testutil

import "time"

// Test timeout constants by test type.
// Use these with context.WithTimeout for consistent, explicit timeouts.
const (
	// TimeoutUnit is for unit tests (no I/O, no external dependencies)
	TimeoutUnit = 30 * time.Second

	// TimeoutIntegration is for integration tests (disk operations, container builds)
	TimeoutIntegration = 2 * time.Minute

	// TimeoutVM is the overall timeout for VM-based tests
	TimeoutVM = 10 * time.Minute

	// TimeoutVMBoot is specifically for waiting for VM boot completion
	TimeoutVMBoot = 2 * time.Minute

	// TimeoutVMInstall is for nbc install operations inside VM
	TimeoutVMInstall = 15 * time.Minute

	// TimeoutOperation is the default timeout for individual Incus operations
	TimeoutOperation = 60 * time.Second
)
