// Package types provides JSON output types for nbc commands.
//
// This package is intended for use by external applications that want to
// parse nbc's JSON output programmatically. All types are serializable
// to JSON and match the structure of nbc's --json output.
//
// Example usage:
//
//	import "github.com/frostyard/nbc/pkg/types"
//
//	// Parse nbc status --json output
//	var status types.StatusOutput
//	json.Unmarshal(data, &status)
//
//	// Parse nbc list --json output
//	var list types.ListOutput
//	json.Unmarshal(data, &list)
package types

// =============================================================================
// Progress Events (Streaming JSON Lines)
// =============================================================================

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

// ProgressEvent represents a single line of JSON Lines output for streaming progress.
// Used by install and update commands for real-time progress updates.
type ProgressEvent struct {
	Type       EventType `json:"type"`
	Timestamp  string    `json:"timestamp"`
	Step       int       `json:"step,omitzero"`
	TotalSteps int       `json:"total_steps,omitzero"`
	StepName   string    `json:"step_name,omitempty"`
	Message    string    `json:"message,omitempty"`
	Percent    int       `json:"percent,omitzero"`
	Details    any       `json:"details,omitempty"`
}

// =============================================================================
// Lint Types
// =============================================================================

// LintSeverity represents the severity of a lint issue
type LintSeverity string

const (
	SeverityError   LintSeverity = "error"
	SeverityWarning LintSeverity = "warning"
)

// LintIssue represents a single lint issue found in a container image
type LintIssue struct {
	Check    string       `json:"check"`
	Severity LintSeverity `json:"severity"`
	Message  string       `json:"message"`
	Path     string       `json:"path,omitempty"`
	Fixed    bool         `json:"fixed,omitzero"` // True if the issue was automatically fixed
}

// LintResult contains all issues found by the linter
type LintResult struct {
	Issues     []LintIssue `json:"issues"`
	ErrorCount int         `json:"error_count"`
	WarnCount  int         `json:"warning_count"`
	FixedCount int         `json:"fixed_count,omitzero"`
}

// LintOutput represents the JSON output structure for the lint command
type LintOutput struct {
	Image      string      `json:"image,omitempty"`
	Local      bool        `json:"local,omitzero"`
	Issues     []LintIssue `json:"issues"`
	ErrorCount int         `json:"error_count"`
	WarnCount  int         `json:"warning_count"`
	FixedCount int         `json:"fixed_count,omitzero"`
	Success    bool        `json:"success"`
}

// =============================================================================
// Cache Types
// =============================================================================

// CachedImageMetadata contains metadata about a cached container image
type CachedImageMetadata struct {
	ImageRef            string            `json:"image_ref"`              // Original image reference
	ImageDigest         string            `json:"image_digest"`           // Image manifest digest (sha256:...)
	DownloadDate        string            `json:"download_date"`          // When the image was downloaded
	Architecture        string            `json:"architecture"`           // Image architecture (amd64, arm64, etc.)
	Labels              map[string]string `json:"labels,omitzero"`        // Container image labels
	OSReleasePrettyName string            `json:"os_release_pretty_name"` // PRETTY_NAME from os-release
	OSReleaseVersionID  string            `json:"os_release_version_id"`  // VERSION_ID from os-release
	OSReleaseID         string            `json:"os_release_id"`          // ID from os-release (e.g., debian, fedora)
	SizeBytes           int64             `json:"size_bytes"`             // Total uncompressed size in bytes
}

// CacheListOutput represents the JSON output structure for the cache list command
type CacheListOutput struct {
	CacheType string                `json:"cache_type"`
	CacheDir  string                `json:"cache_dir"`
	Images    []CachedImageMetadata `json:"images"`
}

// =============================================================================
// Download Command Output
// =============================================================================

// DownloadOutput represents the JSON output structure for the download command
type DownloadOutput struct {
	ImageRef     string `json:"image_ref"`
	ImageDigest  string `json:"image_digest"`
	CacheDir     string `json:"cache_dir"`
	SizeBytes    int64  `json:"size_bytes"`
	Architecture string `json:"architecture"`
	OSName       string `json:"os_name,omitempty"`
}

// =============================================================================
// Status Command Output
// =============================================================================

// UpdateCheck represents the update check result in status JSON output
type UpdateCheck struct {
	Available     bool   `json:"available"`
	RemoteDigest  string `json:"remote_digest,omitempty"`
	CurrentDigest string `json:"current_digest,omitempty"`
	Error         string `json:"error,omitempty"`
}

// StagedUpdate represents a pre-downloaded update ready to apply
type StagedUpdate struct {
	ImageRef    string `json:"image_ref"`
	ImageDigest string `json:"image_digest"`
	SizeBytes   int64  `json:"size_bytes"`
	Ready       bool   `json:"ready"` // true if different from installed version
}

// RebootPendingInfo contains information about a pending update awaiting reboot
type RebootPendingInfo struct {
	PendingImageRef    string `json:"pending_image_ref"`
	PendingImageDigest string `json:"pending_image_digest"`
	UpdateTime         string `json:"update_time"`
	TargetPartition    string `json:"target_partition"`
}

// StatusOutput represents the JSON output structure for the status command
type StatusOutput struct {
	Image          string             `json:"image"`
	Digest         string             `json:"digest,omitempty"`
	Device         string             `json:"device"`
	ActiveRoot     string             `json:"active_root,omitempty"`
	ActiveSlot     string             `json:"active_slot,omitempty"`
	RootMountMode  string             `json:"root_mount_mode,omitempty"`
	BootloaderType string             `json:"bootloader_type"`
	FilesystemType string             `json:"filesystem_type"`
	InstallDate    string             `json:"install_date,omitempty"`
	KernelArgs     []string           `json:"kernel_args,omitzero"`
	UpdateCheck    *UpdateCheck       `json:"update_check,omitempty"`
	StagedUpdate   *StagedUpdate      `json:"staged_update,omitempty"`
	RebootPending  *RebootPendingInfo `json:"reboot_pending,omitempty"`
}

// =============================================================================
// List Command Output
// =============================================================================

// PartitionOutput represents a partition in JSON output
type PartitionOutput struct {
	Device     string `json:"device"`
	Size       uint64 `json:"size"`
	SizeHuman  string `json:"size_human"`
	MountPoint string `json:"mount_point,omitempty"`
	FileSystem string `json:"filesystem,omitempty"`
}

// DiskOutput represents a disk in JSON output
type DiskOutput struct {
	Device      string            `json:"device"`
	Size        uint64            `json:"size"`
	SizeHuman   string            `json:"size_human"`
	Model       string            `json:"model,omitempty"`
	IsRemovable bool              `json:"is_removable"`
	Partitions  []PartitionOutput `json:"partitions"`
}

// ListOutput represents the JSON output structure for the list command
type ListOutput struct {
	Disks []DiskOutput `json:"disks"`
}

// =============================================================================
// Update Command Output
// =============================================================================

// UpdateCheckOutput represents the JSON output structure for the update --check command
type UpdateCheckOutput struct {
	UpdateNeeded  bool   `json:"update_needed"`
	Image         string `json:"image"`
	Device        string `json:"device"`
	CurrentDigest string `json:"current_digest,omitempty"`
	NewDigest     string `json:"new_digest,omitempty"`
	Message       string `json:"message,omitempty"`
}

// =============================================================================
// Validate Command Output
// =============================================================================

// ValidateOutput represents the JSON output structure for the validate command
type ValidateOutput struct {
	Device  string `json:"device"`
	Valid   bool   `json:"valid"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}
