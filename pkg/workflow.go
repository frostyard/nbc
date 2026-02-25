package pkg

import (
	"context"
	"fmt"
)

// StepFunc is a single step in a workflow.
type StepFunc func(ctx context.Context, state *WorkflowState) error

type namedStep struct {
	name string
	fn   StepFunc
}

// Workflow orchestrates a sequence of named steps with progress reporting
// and context cancellation.
type Workflow struct {
	steps    []namedStep
	reporter Reporter
}

// NewWorkflow creates a Workflow that reports progress via the given Reporter.
func NewWorkflow(reporter Reporter) *Workflow {
	return &Workflow{reporter: reporter}
}

// AddStep appends a named step to the workflow.
func (w *Workflow) AddStep(name string, fn StepFunc) {
	w.steps = append(w.steps, namedStep{name: name, fn: fn})
}

// Run executes all steps in order. It checks context before each step
// and reports step progress through the Reporter.
func (w *Workflow) Run(ctx context.Context, state *WorkflowState) error {
	total := len(w.steps)
	for i, step := range w.steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		w.reporter.Step(i+1, total, step.name)
		if err := step.fn(ctx, state); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}
	return nil
}

// WorkflowState holds shared mutable state passed between workflow steps.
type WorkflowState struct {
	// Device is the target block device path.
	Device string
	// MountPoint is the temporary mount point for the target filesystem.
	MountPoint string
	// Scheme holds partition layout information.
	Scheme *PartitionScheme
	// ImageRef is the container image reference.
	ImageRef string
	// ImageDigest is the digest of the image being installed/updated.
	ImageDigest string
	// FilesystemType is "ext4" or "btrfs".
	FilesystemType string
	// KernelArgs holds additional kernel command line arguments.
	KernelArgs []string
	// DryRun indicates whether to simulate operations.
	DryRun bool
	// Verbose enables additional output.
	Verbose bool
	// Reporter is the output reporter for the workflow.
	Reporter Reporter
	// Encrypted indicates whether LUKS encryption is enabled.
	Encrypted bool
	// Passphrase is the LUKS passphrase (if encrypted).
	Passphrase string
	// TPM2 indicates whether TPM2 auto-unlock is enabled.
	TPM2 bool
}
