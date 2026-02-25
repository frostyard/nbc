package pkg

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/frostyard/nbc/pkg/types"
)

func TestTextReporter_Step(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf)

	r.Step(1, 3, "Partitioning disk")

	got := buf.String()
	want := "Step 1/3: Partitioning disk...\n"
	if got != want {
		t.Errorf("Step output = %q, want %q", got, want)
	}
}

func TestTextReporter_StepAddsNewlineAfterFirst(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf)

	r.Step(1, 3, "First step")
	r.Step(2, 3, "Second step")
	r.Step(3, 3, "Third step")

	got := buf.String()
	// First step has no leading blank line; subsequent steps do
	want := "Step 1/3: First step...\n\nStep 2/3: Second step...\n\nStep 3/3: Third step...\n"
	if got != want {
		t.Errorf("Step output = %q, want %q", got, want)
	}
}

func TestTextReporter_Progress(t *testing.T) {
	t.Run("non-empty message", func(t *testing.T) {
		var buf bytes.Buffer
		r := NewTextReporter(&buf)

		r.Progress(50, "Halfway there")

		got := buf.String()
		want := "  Halfway there\n"
		if got != want {
			t.Errorf("Progress output = %q, want %q", got, want)
		}
	})

	t.Run("empty message prints nothing", func(t *testing.T) {
		var buf bytes.Buffer
		r := NewTextReporter(&buf)

		r.Progress(50, "")

		got := buf.String()
		if got != "" {
			t.Errorf("Progress with empty message should produce no output, got %q", got)
		}
	})
}

func TestTextReporter_Message(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf)

	r.Message("Installing %s version %d", "GRUB", 2)

	got := buf.String()
	want := "  Installing GRUB version 2\n"
	if got != want {
		t.Errorf("Message output = %q, want %q", got, want)
	}
}

func TestTextReporter_MessagePlain(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf)

	r.MessagePlain("No indentation %s", "here")

	got := buf.String()
	want := "No indentation here\n"
	if got != want {
		t.Errorf("MessagePlain output = %q, want %q", got, want)
	}
}

func TestTextReporter_Warning(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf)

	r.Warning("disk %s is small", "/dev/sda")

	got := buf.String()
	want := "Warning: disk /dev/sda is small\n"
	if got != want {
		t.Errorf("Warning output = %q, want %q", got, want)
	}
}

func TestTextReporter_Error(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf)

	r.Error(errors.New("permission denied"), "failed to write")

	got := buf.String()
	want := "Error: failed to write: permission denied\n"
	if got != want {
		t.Errorf("Error output = %q, want %q", got, want)
	}
}

func TestTextReporter_Complete(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf)

	r.Complete("Installation complete!", nil)

	got := buf.String()
	sep := "================================================================="
	want := "\n" + sep + "\n" + "Installation complete!" + "\n" + sep + "\n"
	if got != want {
		t.Errorf("Complete output = %q, want %q", got, want)
	}
}

func TestTextReporter_IsJSON(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf)

	if r.IsJSON() {
		t.Error("TextReporter.IsJSON() = true, want false")
	}
}

func TestJSONReporter_Step(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONReporter(&buf)

	r.Step(2, 5, "Formatting partitions")

	var event types.ProgressEvent
	if err := json.Unmarshal(buf.Bytes(), &event); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if event.Type != types.EventTypeStep {
		t.Errorf("event.Type = %q, want %q", event.Type, types.EventTypeStep)
	}
	if event.Step != 2 {
		t.Errorf("event.Step = %d, want 2", event.Step)
	}
	if event.TotalSteps != 5 {
		t.Errorf("event.TotalSteps = %d, want 5", event.TotalSteps)
	}
	if event.StepName != "Formatting partitions" {
		t.Errorf("event.StepName = %q, want %q", event.StepName, "Formatting partitions")
	}
	if event.Timestamp == "" {
		t.Error("event.Timestamp should not be empty")
	}
}

func TestJSONReporter_Message(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONReporter(&buf)

	r.Message("hello %s", "world")

	var event types.ProgressEvent
	if err := json.Unmarshal(buf.Bytes(), &event); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if event.Type != types.EventTypeMessage {
		t.Errorf("event.Type = %q, want %q", event.Type, types.EventTypeMessage)
	}
	if event.Message != "hello world" {
		t.Errorf("event.Message = %q, want %q", event.Message, "hello world")
	}
}

func TestJSONReporter_Warning(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONReporter(&buf)

	r.Warning("low disk space on %s", "/dev/sda")

	var event types.ProgressEvent
	if err := json.Unmarshal(buf.Bytes(), &event); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if event.Type != types.EventTypeWarning {
		t.Errorf("event.Type = %q, want %q", event.Type, types.EventTypeWarning)
	}
	if event.Message != "low disk space on /dev/sda" {
		t.Errorf("event.Message = %q, want %q", event.Message, "low disk space on /dev/sda")
	}
}

func TestJSONReporter_Progress(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONReporter(&buf)

	r.Progress(75, "extracting layers")

	var event types.ProgressEvent
	if err := json.Unmarshal(buf.Bytes(), &event); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if event.Type != types.EventTypeProgress {
		t.Errorf("event.Type = %q, want %q", event.Type, types.EventTypeProgress)
	}
	if event.Percent != 75 {
		t.Errorf("event.Percent = %d, want 75", event.Percent)
	}
	if event.Message != "extracting layers" {
		t.Errorf("event.Message = %q, want %q", event.Message, "extracting layers")
	}
}

func TestJSONReporter_Error(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONReporter(&buf)

	r.Error(errors.New("disk full"), "write failed")

	var event types.ProgressEvent
	if err := json.Unmarshal(buf.Bytes(), &event); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if event.Type != types.EventTypeError {
		t.Errorf("event.Type = %q, want %q", event.Type, types.EventTypeError)
	}
	if event.Message != "write failed" {
		t.Errorf("event.Message = %q, want %q", event.Message, "write failed")
	}

	// Details should contain the error string
	details, ok := event.Details.(map[string]any)
	if !ok {
		t.Fatalf("event.Details is %T, want map[string]any", event.Details)
	}
	if details["error"] != "disk full" {
		t.Errorf("event.Details[error] = %q, want %q", details["error"], "disk full")
	}
}

func TestJSONReporter_Complete(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONReporter(&buf)

	r.Complete("done", map[string]string{"device": "/dev/sda"})

	var event types.ProgressEvent
	if err := json.Unmarshal(buf.Bytes(), &event); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if event.Type != types.EventTypeComplete {
		t.Errorf("event.Type = %q, want %q", event.Type, types.EventTypeComplete)
	}
	if event.Message != "done" {
		t.Errorf("event.Message = %q, want %q", event.Message, "done")
	}
}

func TestJSONReporter_MultipleEvents(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONReporter(&buf)

	r.Step(1, 2, "First")
	r.Message("info")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSON lines, got %d: %q", len(lines), buf.String())
	}

	var event1 types.ProgressEvent
	if err := json.Unmarshal([]byte(lines[0]), &event1); err != nil {
		t.Fatalf("failed to parse first JSON line: %v", err)
	}
	if event1.Type != types.EventTypeStep {
		t.Errorf("first event type = %q, want %q", event1.Type, types.EventTypeStep)
	}

	var event2 types.ProgressEvent
	if err := json.Unmarshal([]byte(lines[1]), &event2); err != nil {
		t.Fatalf("failed to parse second JSON line: %v", err)
	}
	if event2.Type != types.EventTypeMessage {
		t.Errorf("second event type = %q, want %q", event2.Type, types.EventTypeMessage)
	}
}

func TestJSONReporter_IsJSON(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONReporter(&buf)

	if !r.IsJSON() {
		t.Error("JSONReporter.IsJSON() = false, want true")
	}
}

func TestNoopReporter(t *testing.T) {
	// NoopReporter should not panic on any method call
	r := NoopReporter{}

	r.Step(1, 3, "test")
	r.Progress(50, "test")
	r.Message("hello %s", "world")
	r.MessagePlain("hello %s", "world")
	r.Warning("careful %s", "now")
	r.Error(errors.New("boom"), "oops")
	r.Complete("done", nil)

	if r.IsJSON() {
		t.Error("NoopReporter.IsJSON() = true, want false")
	}
}
