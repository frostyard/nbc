package pkg

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// EventType represents the type of progress event
type EventType string

const (
	EventTypeStep     EventType = "step"
	EventTypeProgress EventType = "progress"
	EventTypeMessage  EventType = "message"
	EventTypeWarning  EventType = "warning"
	EventTypeError    EventType = "error"
	EventTypeComplete EventType = "complete"
)

// ProgressEvent represents a single line of JSON Lines output for streaming progress
type ProgressEvent struct {
	Type       EventType `json:"type"`
	Timestamp  string    `json:"timestamp"`
	Step       int       `json:"step,omitempty"`
	TotalSteps int       `json:"total_steps,omitempty"`
	StepName   string    `json:"step_name,omitempty"`
	Message    string    `json:"message,omitempty"`
	Percent    int       `json:"percent,omitempty"`
	Details    any       `json:"details,omitempty"`
}

// ProgressReporter handles streaming JSON Lines output for progress updates
type ProgressReporter struct {
	mu         sync.Mutex
	enabled    bool
	totalSteps int
	encoder    *json.Encoder
}

// NewProgressReporter creates a new progress reporter
func NewProgressReporter(jsonEnabled bool, totalSteps int) *ProgressReporter {
	return &ProgressReporter{
		enabled:    jsonEnabled,
		totalSteps: totalSteps,
		encoder:    json.NewEncoder(os.Stdout),
	}
}

// emit sends a single JSON event to stdout
func (p *ProgressReporter) emit(event ProgressEvent) {
	if !p.enabled {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	event.Timestamp = time.Now().UTC().Format(time.RFC3339)
	_ = p.encoder.Encode(event)
}

// Step reports the start of a new step
func (p *ProgressReporter) Step(step int, name string) {
	if p.enabled {
		p.emit(ProgressEvent{
			Type:       EventTypeStep,
			Step:       step,
			TotalSteps: p.totalSteps,
			StepName:   name,
		})
	} else {
		if step > 1 {
			fmt.Println()
		}
		fmt.Printf("Step %d/%d: %s...\n", step, p.totalSteps, name)
	}
}

// Progress reports progress within a step (0-100 percent)
func (p *ProgressReporter) Progress(percent int, message string) {
	if p.enabled {
		p.emit(ProgressEvent{
			Type:    EventTypeProgress,
			Percent: percent,
			Message: message,
		})
	} else if message != "" {
		fmt.Printf("  %s\n", message)
	}
}

// Message reports an informational message
func (p *ProgressReporter) Message(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if p.enabled {
		p.emit(ProgressEvent{
			Type:    EventTypeMessage,
			Message: msg,
		})
	} else {
		fmt.Printf("  %s\n", msg)
	}
}

// MessagePlain reports an informational message without indentation (for non-JSON)
func (p *ProgressReporter) MessagePlain(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if p.enabled {
		p.emit(ProgressEvent{
			Type:    EventTypeMessage,
			Message: msg,
		})
	} else {
		fmt.Println(msg)
	}
}

// Warning reports a warning message
func (p *ProgressReporter) Warning(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if p.enabled {
		p.emit(ProgressEvent{
			Type:    EventTypeWarning,
			Message: msg,
		})
	} else {
		fmt.Printf("Warning: %s\n", msg)
	}
}

// Error reports an error
func (p *ProgressReporter) Error(err error, message string) {
	if p.enabled {
		p.emit(ProgressEvent{
			Type:    EventTypeError,
			Message: message,
			Details: map[string]string{"error": err.Error()},
		})
	} else {
		fmt.Printf("Error: %s: %v\n", message, err)
	}
}

// Complete reports successful completion with optional result details
func (p *ProgressReporter) Complete(message string, details any) {
	if p.enabled {
		p.emit(ProgressEvent{
			Type:    EventTypeComplete,
			Message: message,
			Details: details,
		})
	} else {
		fmt.Println()
		fmt.Println("=================================================================")
		fmt.Println(message)
		fmt.Println("=================================================================")
	}
}

// IsJSON returns true if JSON output is enabled
func (p *ProgressReporter) IsJSON() bool {
	return p.enabled
}
