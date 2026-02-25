package pkg

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/frostyard/nbc/pkg/types"
)

// Reporter is the interface for reporting progress and messages during
// install and update operations. It has three implementations:
//   - TextReporter: human-readable text output
//   - JSONReporter: machine-readable JSON Lines output
//   - NoopReporter: silently discards all output
type Reporter interface {
	Step(step, total int, name string)
	Progress(percent int, message string)
	Message(format string, args ...any)
	MessagePlain(format string, args ...any)
	Warning(format string, args ...any)
	Error(err error, message string)
	Complete(message string, details any)
	IsJSON() bool
}

// ---------------------------------------------------------------------------
// TextReporter
// ---------------------------------------------------------------------------

// TextReporter writes human-readable progress text to an io.Writer.
type TextReporter struct {
	w       io.Writer
	stepped bool // true after the first Step call
}

// NewTextReporter returns a TextReporter that writes to w.
func NewTextReporter(w io.Writer) *TextReporter {
	return &TextReporter{w: w}
}

func (r *TextReporter) Step(step, total int, name string) {
	if r.stepped {
		_, _ = fmt.Fprintln(r.w)
	}
	r.stepped = true
	_, _ = fmt.Fprintf(r.w, "Step %d/%d: %s...\n", step, total, name)
}

func (r *TextReporter) Progress(_ int, message string) {
	if message != "" {
		_, _ = fmt.Fprintf(r.w, "  %s\n", message)
	}
}

func (r *TextReporter) Message(format string, args ...any) {
	_, _ = fmt.Fprintf(r.w, "  %s\n", fmt.Sprintf(format, args...))
}

func (r *TextReporter) MessagePlain(format string, args ...any) {
	_, _ = fmt.Fprintln(r.w, fmt.Sprintf(format, args...))
}

func (r *TextReporter) Warning(format string, args ...any) {
	_, _ = fmt.Fprintf(r.w, "Warning: %s\n", fmt.Sprintf(format, args...))
}

func (r *TextReporter) Error(err error, message string) {
	_, _ = fmt.Fprintf(r.w, "Error: %s: %v\n", message, err)
}

func (r *TextReporter) Complete(message string, _ any) {
	_, _ = fmt.Fprintln(r.w)
	_, _ = fmt.Fprintln(r.w, "=================================================================")
	_, _ = fmt.Fprintln(r.w, message)
	_, _ = fmt.Fprintln(r.w, "=================================================================")
}

func (r *TextReporter) IsJSON() bool { return false }

// ---------------------------------------------------------------------------
// JSONReporter
// ---------------------------------------------------------------------------

// JSONReporter writes JSON Lines (one types.ProgressEvent per line) to an
// io.Writer. All writes are serialized with a mutex for thread safety.
type JSONReporter struct {
	mu      sync.Mutex
	encoder *json.Encoder
}

// NewJSONReporter returns a JSONReporter that writes to w.
func NewJSONReporter(w io.Writer) *JSONReporter {
	return &JSONReporter{encoder: json.NewEncoder(w)}
}

func (r *JSONReporter) emit(event types.ProgressEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	event.Timestamp = time.Now().UTC().Format(time.RFC3339)
	_ = r.encoder.Encode(event)
}

func (r *JSONReporter) Step(step, total int, name string) {
	r.emit(types.ProgressEvent{
		Type:       types.EventTypeStep,
		Step:       step,
		TotalSteps: total,
		StepName:   name,
	})
}

func (r *JSONReporter) Progress(percent int, message string) {
	r.emit(types.ProgressEvent{
		Type:    types.EventTypeProgress,
		Percent: percent,
		Message: message,
	})
}

func (r *JSONReporter) Message(format string, args ...any) {
	r.emit(types.ProgressEvent{
		Type:    types.EventTypeMessage,
		Message: fmt.Sprintf(format, args...),
	})
}

func (r *JSONReporter) MessagePlain(format string, args ...any) {
	r.emit(types.ProgressEvent{
		Type:    types.EventTypeMessage,
		Message: fmt.Sprintf(format, args...),
	})
}

func (r *JSONReporter) Warning(format string, args ...any) {
	r.emit(types.ProgressEvent{
		Type:    types.EventTypeWarning,
		Message: fmt.Sprintf(format, args...),
	})
}

func (r *JSONReporter) Error(err error, message string) {
	r.emit(types.ProgressEvent{
		Type:    types.EventTypeError,
		Message: message,
		Details: map[string]string{"error": err.Error()},
	})
}

func (r *JSONReporter) Complete(message string, details any) {
	r.emit(types.ProgressEvent{
		Type:    types.EventTypeComplete,
		Message: message,
		Details: details,
	})
}

func (r *JSONReporter) IsJSON() bool { return true }

// ---------------------------------------------------------------------------
// NoopReporter
// ---------------------------------------------------------------------------

// NoopReporter silently discards all output. Useful for tests and contexts
// where no progress reporting is needed.
type NoopReporter struct{}

func (NoopReporter) Step(int, int, string)       {}
func (NoopReporter) Progress(int, string)        {}
func (NoopReporter) Message(string, ...any)      {}
func (NoopReporter) MessagePlain(string, ...any) {}
func (NoopReporter) Warning(string, ...any)      {}
func (NoopReporter) Error(error, string)         {}
func (NoopReporter) Complete(string, any)        {}
func (NoopReporter) IsJSON() bool                { return false }
