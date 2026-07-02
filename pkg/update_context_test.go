package pkg

import (
	"context"
	"errors"
	"testing"

	"github.com/frostyard/std/reporter"
)

// TestUpdate_RespectsCancelledContext verifies that an already-cancelled context
// aborts the update immediately, before any disk work -- the core of #94.
func TestUpdate_RespectsCancelledContext(t *testing.T) {
	u := &SystemUpdater{Progress: reporter.NoopReporter{}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := u.Update(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Update(cancelled ctx) = %v, want context.Canceled", err)
	}
}

// TestPerformUpdate_RespectsCancelledContext verifies PerformUpdate bails out on
// a cancelled context before acquiring the system lock or doing any work.
func TestPerformUpdate_RespectsCancelledContext(t *testing.T) {
	u := &SystemUpdater{Progress: reporter.NoopReporter{}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := u.PerformUpdate(ctx, true); !errors.Is(err, context.Canceled) {
		t.Errorf("PerformUpdate(cancelled ctx) = %v, want context.Canceled", err)
	}
}
