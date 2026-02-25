package pkg

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stepRecord struct {
	step, total int
	name        string
}

type stepTrackingReporter struct {
	NoopReporter
	steps []stepRecord
}

func (r *stepTrackingReporter) Step(step, total int, name string) {
	r.steps = append(r.steps, stepRecord{step, total, name})
}

func TestWorkflow_RunsAllSteps(t *testing.T) {
	var ran []string
	w := NewWorkflow(&NoopReporter{})
	w.AddStep("step1", func(_ context.Context, _ *WorkflowState) error {
		ran = append(ran, "step1")
		return nil
	})
	w.AddStep("step2", func(_ context.Context, _ *WorkflowState) error {
		ran = append(ran, "step2")
		return nil
	})

	if err := w.Run(context.Background(), &WorkflowState{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ran) != 2 || ran[0] != "step1" || ran[1] != "step2" {
		t.Errorf("ran = %v, want [step1 step2]", ran)
	}
}

func TestWorkflow_StopsOnError(t *testing.T) {
	var ran []string
	w := NewWorkflow(&NoopReporter{})
	w.AddStep("good", func(_ context.Context, _ *WorkflowState) error {
		ran = append(ran, "good")
		return nil
	})
	w.AddStep("bad", func(_ context.Context, _ *WorkflowState) error {
		return errors.New("boom")
	})
	w.AddStep("never", func(_ context.Context, _ *WorkflowState) error {
		ran = append(ran, "never")
		return nil
	})

	err := w.Run(context.Background(), &WorkflowState{})
	if err == nil {
		t.Fatal("expected error")
	}
	if len(ran) != 1 || ran[0] != "good" {
		t.Errorf("ran = %v, want [good]", ran)
	}
}

func TestWorkflow_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var ran bool
	w := NewWorkflow(&NoopReporter{})
	w.AddStep("unreachable", func(_ context.Context, _ *WorkflowState) error {
		ran = true
		return nil
	})

	err := w.Run(ctx, &WorkflowState{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if ran {
		t.Error("step should not have run with cancelled context")
	}
}

func TestWorkflow_ReportsSteps(t *testing.T) {
	reporter := &stepTrackingReporter{}
	w := NewWorkflow(reporter)
	w.AddStep("First", func(_ context.Context, _ *WorkflowState) error { return nil })
	w.AddStep("Second", func(_ context.Context, _ *WorkflowState) error { return nil })

	if err := w.Run(context.Background(), &WorkflowState{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reporter.steps) != 2 {
		t.Fatalf("expected 2 step reports, got %d", len(reporter.steps))
	}
	if reporter.steps[0].step != 1 || reporter.steps[0].total != 2 || reporter.steps[0].name != "First" {
		t.Errorf("step[0] = %+v, want {1, 2, First}", reporter.steps[0])
	}
	if reporter.steps[1].step != 2 || reporter.steps[1].total != 2 || reporter.steps[1].name != "Second" {
		t.Errorf("step[1] = %+v, want {2, 2, Second}", reporter.steps[1])
	}
}

func TestWorkflow_ErrorWrapsStepName(t *testing.T) {
	w := NewWorkflow(&NoopReporter{})
	w.AddStep("Format disks", func(_ context.Context, _ *WorkflowState) error {
		return errors.New("disk full")
	})

	err := w.Run(context.Background(), &WorkflowState{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Format disks") {
		t.Errorf("error %q should contain step name %q", err.Error(), "Format disks")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("error %q should contain original error %q", err.Error(), "disk full")
	}
}

func TestWorkflow_EmptyWorkflow(t *testing.T) {
	w := NewWorkflow(&NoopReporter{})
	if err := w.Run(context.Background(), &WorkflowState{}); err != nil {
		t.Fatalf("empty workflow should succeed, got: %v", err)
	}
}
