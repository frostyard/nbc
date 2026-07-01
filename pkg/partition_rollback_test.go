package pkg

import (
	"context"
	"strings"
	"testing"

	"github.com/frostyard/std/reporter"
)

// TestMountPartitions_RollsBackOnPartialFailure is the regression test for #92.
// If a later mount fails (e.g. the boot partition), the partitions already
// mounted (root1) must be unmounted before returning, so a partial failure does
// not leak mounts and block a retried install on the busy mountpoint.
func TestMountPartitions_RollsBackOnPartialFailure(t *testing.T) {
	var mounted []string
	var unmounted []string

	origMount, origUmount := mountCommand, umountCommand
	t.Cleanup(func() { mountCommand, umountCommand = origMount, origUmount })

	mountCommand = func(_ context.Context, _, target string) error {
		// Simulate the boot partition mount failing after root1 succeeds.
		if strings.HasSuffix(target, "/boot") {
			return &mountError{}
		}
		mounted = append(mounted, target)
		return nil
	}
	umountCommand = func(_ context.Context, target string) error {
		unmounted = append(unmounted, target)
		return nil
	}

	scheme := &PartitionScheme{
		BootPartition:  "/dev/fake-boot",
		Root1Partition: "/dev/fake-root1",
		VarPartition:   "/dev/fake-var",
	}
	mountPoint := t.TempDir()

	err := MountPartitions(context.Background(), scheme, mountPoint, false, reporter.NoopReporter{})
	if err == nil {
		t.Fatal("expected an error when the boot mount fails")
	}

	// root1 was mounted at mountPoint and must be rolled back; boot failed (not
	// mounted); var was never attempted.
	if len(unmounted) != 1 || unmounted[0] != mountPoint {
		t.Errorf("expected rollback to unmount %q exactly once, got: %v", mountPoint, unmounted)
	}
}

type mountError struct{}

func (mountError) Error() string { return "simulated mount failure" }
