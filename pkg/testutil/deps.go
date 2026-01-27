// Package testutil provides test helpers and fixtures for nbc testing.
//
// This file imports test infrastructure dependencies to ensure they are
// tracked in go.mod. These will be used by the Incus fixture and golden
// file helpers in subsequent plans.
package testutil

import (
	// Incus Go client for VM management in integration tests
	_ "github.com/lxc/incus/v6/client"

	// Goldie for golden file testing with -update flag support
	_ "github.com/sebdah/goldie/v2"
)
