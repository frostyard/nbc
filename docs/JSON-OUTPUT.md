# JSON Output Format for Machine-Readable Output

nbc supports streaming JSON Lines (JSONL) output for machine-readable consumption by installers, update tools, and automation systems. This document describes the output format and how to consume it.

## Enabling JSON Output

Add the `--json` flag to any command:

```bash
nbc list --json
nbc install --image myimage:latest --device /dev/sda --json
nbc update --json
nbc status --json
nbc validate --device /dev/sda --json
```

## Output Format

### Simple Commands (list, status, validate)

For simple query commands, nbc outputs a single JSON object with the complete result:

```bash
nbc list --json
```

```json
{
  "disks": [
    {
      "device": "/dev/sda",
      "size": 500107862016,
      "size_human": "465.8 GB",
      "model": "Samsung SSD 860",
      "is_removable": false,
      "partitions": [
        {
          "device": "/dev/sda1",
          "size": 536870912,
          "size_human": "512.0 MB",
          "mount_point": "/boot/efi",
          "filesystem": "vfat"
        }
      ]
    }
  ]
}
```

### Streaming Commands (install, update)

For long-running operations, nbc outputs **JSON Lines** (one JSON object per line) to provide real-time progress updates:

```bash
nbc install --image myimage:latest --device /dev/sda --json
```

```jsonl
{"type":"message","timestamp":"2025-12-19T10:30:00Z","message":"Checking prerequisites..."}
{"type":"message","timestamp":"2025-12-19T10:30:01Z","message":"Validating disk /dev/sda..."}
{"type":"step","timestamp":"2025-12-19T10:30:02Z","step":1,"total_steps":6,"step_name":"Creating partitions"}
{"type":"message","timestamp":"2025-12-19T10:30:03Z","message":"Created EFI partition"}
{"type":"step","timestamp":"2025-12-19T10:30:10Z","step":2,"total_steps":6,"step_name":"Formatting partitions"}
{"type":"step","timestamp":"2025-12-19T10:30:20Z","step":3,"total_steps":6,"step_name":"Mounting partitions"}
{"type":"step","timestamp":"2025-12-19T10:30:21Z","step":4,"total_steps":6,"step_name":"Extracting container filesystem"}
{"type":"step","timestamp":"2025-12-19T10:31:00Z","step":5,"total_steps":6,"step_name":"Configuring system"}
{"type":"step","timestamp":"2025-12-19T10:31:10Z","step":6,"total_steps":6,"step_name":"Installing bootloader"}
{"type":"complete","timestamp":"2025-12-19T10:31:20Z","message":"Installation complete! You can now boot from this disk.","details":{"image":"myimage:latest","device":"/dev/sda","filesystem":"ext4"}}
```

## Event Types

Each JSON line contains a `type` field indicating the event type:

| Type       | Description                                     |
| ---------- | ----------------------------------------------- |
| `step`     | Start of a numbered step in the operation       |
| `progress` | Progress update within a step (with percentage) |
| `message`  | Informational message                           |
| `warning`  | Warning that doesn't stop the operation         |
| `error`    | Error that caused the operation to fail         |
| `complete` | Successful completion of the entire operation   |

### Event Schema

#### Step Event

```json
{
  "type": "step",
  "timestamp": "2025-12-19T10:30:02Z",
  "step": 1,
  "total_steps": 6,
  "step_name": "Creating partitions"
}
```

| Field         | Type    | Description                     |
| ------------- | ------- | ------------------------------- |
| `type`        | string  | Always `"step"`                 |
| `timestamp`   | string  | ISO 8601 timestamp (UTC)        |
| `step`        | integer | Current step number (1-based)   |
| `total_steps` | integer | Total number of steps           |
| `step_name`   | string  | Human-readable step description |

#### Progress Event

```json
{
  "type": "progress",
  "timestamp": "2025-12-19T10:30:30Z",
  "percent": 45,
  "message": "Extracting layer 3/5"
}
```

| Field       | Type    | Description                   |
| ----------- | ------- | ----------------------------- |
| `type`      | string  | Always `"progress"`           |
| `timestamp` | string  | ISO 8601 timestamp (UTC)      |
| `percent`   | integer | Progress percentage (0-100)   |
| `message`   | string  | Optional progress description |

#### Message Event

```json
{
  "type": "message",
  "timestamp": "2025-12-19T10:30:05Z",
  "message": "Image reference is valid and accessible"
}
```

| Field       | Type   | Description              |
| ----------- | ------ | ------------------------ |
| `type`      | string | Always `"message"`       |
| `timestamp` | string | ISO 8601 timestamp (UTC) |
| `message`   | string | Informational message    |

#### Warning Event

```json
{
  "type": "warning",
  "timestamp": "2025-12-19T10:30:15Z",
  "message": "Could not get image digest: network timeout"
}
```

| Field       | Type   | Description              |
| ----------- | ------ | ------------------------ |
| `type`      | string | Always `"warning"`       |
| `timestamp` | string | ISO 8601 timestamp (UTC) |
| `message`   | string | Warning description      |

#### Error Event

```json
{
  "type": "error",
  "timestamp": "2025-12-19T10:30:20Z",
  "message": "Installation failed",
  "details": {
    "error": "failed to create partitions: device is busy"
  }
}
```

| Field       | Type   | Description              |
| ----------- | ------ | ------------------------ |
| `type`      | string | Always `"error"`         |
| `timestamp` | string | ISO 8601 timestamp (UTC) |
| `message`   | string | Error summary            |
| `details`   | object | Additional error details |

#### Complete Event

```json
{
  "type": "complete",
  "timestamp": "2025-12-19T10:31:20Z",
  "message": "Installation complete! You can now boot from this disk.",
  "details": {
    "image": "myimage:latest",
    "device": "/dev/sda",
    "filesystem": "ext4"
  }
}
```

| Field       | Type   | Description                    |
| ----------- | ------ | ------------------------------ |
| `type`      | string | Always `"complete"`            |
| `timestamp` | string | ISO 8601 timestamp (UTC)       |
| `message`   | string | Completion message             |
| `details`   | object | Operation-specific result data |

## Consuming JSON Output

### Shell Script Example

```bash
#!/bin/bash

nbc install --image "$IMAGE" --device "$DEVICE" --json | while IFS= read -r line; do
    type=$(echo "$line" | jq -r '.type')

    case "$type" in
        step)
            step=$(echo "$line" | jq -r '.step')
            total=$(echo "$line" | jq -r '.total_steps')
            name=$(echo "$line" | jq -r '.step_name')
            echo "[$step/$total] $name"
            ;;
        message)
            echo "  $(echo "$line" | jq -r '.message')"
            ;;
        warning)
            echo "WARNING: $(echo "$line" | jq -r '.message')" >&2
            ;;
        error)
            echo "ERROR: $(echo "$line" | jq -r '.message')" >&2
            exit 1
            ;;
        complete)
            echo "SUCCESS: $(echo "$line" | jq -r '.message')"
            ;;
    esac
done
```

### Python Example

```python
#!/usr/bin/env python3
import subprocess
import json
import sys

def run_nbc_install(image: str, device: str):
    proc = subprocess.Popen(
        ["nbc", "install", "--image", image, "--device", device, "--json"],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True
    )

    for line in proc.stdout:
        event = json.loads(line.strip())
        event_type = event.get("type")

        if event_type == "step":
            step = event["step"]
            total = event["total_steps"]
            name = event["step_name"]
            print(f"[{step}/{total}] {name}")

        elif event_type == "progress":
            percent = event.get("percent", 0)
            message = event.get("message", "")
            print(f"  {percent}% - {message}")

        elif event_type == "message":
            print(f"  {event['message']}")

        elif event_type == "warning":
            print(f"WARNING: {event['message']}", file=sys.stderr)

        elif event_type == "error":
            print(f"ERROR: {event['message']}", file=sys.stderr)
            details = event.get("details", {})
            if "error" in details:
                print(f"  Details: {details['error']}", file=sys.stderr)
            return False

        elif event_type == "complete":
            print(f"\nSUCCESS: {event['message']}")
            details = event.get("details", {})
            return True

    proc.wait()
    return proc.returncode == 0

if __name__ == "__main__":
    success = run_nbc_install("myimage:latest", "/dev/sda")
    sys.exit(0 if success else 1)
```

### Go Example

```go
package main

import (
    "bufio"
    "encoding/json"
    "fmt"
    "os/exec"

    "github.com/frostyard/nbc/pkg/types"
)

func runNbcInstall(image, device string) error {
    cmd := exec.Command("nbc", "install", "--image", image, "--device", device, "--json")
    stdout, err := cmd.StdoutPipe()
    if err != nil {
        return err
    }

    if err := cmd.Start(); err != nil {
        return err
    }

    scanner := bufio.NewScanner(stdout)
    for scanner.Scan() {
        var event types.ProgressEvent
        if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
            continue
        }

        switch event.Type {
        case types.EventTypeStep:
            fmt.Printf("[%d/%d] %s\n", event.Step, event.TotalSteps, event.StepName)
        case types.EventTypeMessage:
            fmt.Printf("  %s\n", event.Message)
        case types.EventTypeWarning:
            fmt.Printf("WARNING: %s\n", event.Message)
        case types.EventTypeError:
            return fmt.Errorf("%s", event.Message)
        case types.EventTypeComplete:
            fmt.Printf("\nSUCCESS: %s\n", event.Message)
        }
    }

    return cmd.Wait()
}
```

## Using nbc Types in Go Applications

The `github.com/frostyard/nbc/pkg/types` package provides all JSON output types for programmatic consumption. This allows Go applications to parse nbc's JSON output with proper type safety.

### Available Types

| Type                  | Description                                       |
| --------------------- | ------------------------------------------------- |
| `ProgressEvent`       | Streaming progress event (install/update)         |
| `EventType`           | Event type constants (step, message, error, etc.) |
| `StatusOutput`        | Output from `nbc status --json`                   |
| `ListOutput`          | Output from `nbc list --json`                     |
| `DiskOutput`          | Disk information within ListOutput                |
| `PartitionOutput`     | Partition information within DiskOutput           |
| `UpdateCheckOutput`   | Output from `nbc update --check --json`           |
| `ValidateOutput`      | Output from `nbc validate --json`                 |
| `DownloadOutput`      | Output from `nbc download --json`                 |
| `CacheListOutput`     | Output from `nbc cache list --json`               |
| `CachedImageMetadata` | Metadata for cached container images              |
| `LintOutput`          | Output from `nbc lint --json`                     |
| `LintIssue`           | Individual lint issue                             |
| `LintResult`          | Aggregated lint results                           |

### Example: Parsing Status Output

```go
package main

import (
    "encoding/json"
    "fmt"
    "os/exec"

    "github.com/frostyard/nbc/pkg/types"
)

func getStatus() (*types.StatusOutput, error) {
    cmd := exec.Command("nbc", "status", "--json")
    output, err := cmd.Output()
    if err != nil {
        return nil, err
    }

    var status types.StatusOutput
    if err := json.Unmarshal(output, &status); err != nil {
        return nil, err
    }

    return &status, nil
}

func main() {
    status, err := getStatus()
    if err != nil {
        fmt.Printf("Error: %v\n", err)
        return
    }

    fmt.Printf("Image: %s\n", status.Image)
    fmt.Printf("Device: %s\n", status.Device)
    fmt.Printf("Active Slot: %s\n", status.ActiveSlot)

    if status.UpdateCheck != nil && status.UpdateCheck.Available {
        fmt.Println("Update available!")
    }
}
```

### Example: Listing Cached Images

```go
package main

import (
    "encoding/json"
    "fmt"
    "os/exec"

    "github.com/frostyard/nbc/pkg/types"
)

func listCachedImages() ([]types.CachedImageMetadata, error) {
    cmd := exec.Command("nbc", "cache", "list", "--install-images", "--json")
    output, err := cmd.Output()
    if err != nil {
        return nil, err
    }

    var cacheOutput types.CacheListOutput
    if err := json.Unmarshal(output, &cacheOutput); err != nil {
        return nil, err
    }

    return cacheOutput.Images, nil
}
```

## Command-Specific Output

### `nbc update --check --json`

The `--check` flag outputs a single JSON object (not streaming):

```json
{
  "update_needed": true,
  "image": "myimage:latest",
  "device": "/dev/sda",
  "current_digest": "sha256:abc123...",
  "new_digest": "sha256:def456...",
  "message": "Update available"
}
```

### `nbc status --json`

```json
{
  "image": "myimage:latest",
  "digest": "sha256:abc123...",
  "device": "/dev/sda",
  "active_root": "/dev/sda3",
  "active_slot": "A (root1)",
  "bootloader_type": "grub2",
  "filesystem_type": "ext4",
  "install_date": "2025-12-19T10:00:00Z",
  "kernel_args": ["console=ttyS0"],
  "update_check": {
    "available": false,
    "remote_digest": "sha256:abc123...",
    "current_digest": "sha256:abc123..."
  }
}
```

### `nbc validate --json`

```json
{
  "device": "/dev/sda",
  "valid": true,
  "message": "Device is valid for bootc installation"
}
```

Or on error:

```json
{
  "device": "/dev/sda",
  "valid": false,
  "error": "device size 8GB is less than minimum required 10GB"
}
```

## Important Notes

1. **Interactive Prompts**: When using `--json`, interactive confirmation prompts are skipped. Use this flag only in automated environments where you've validated the operation is safe.

2. **Exit Codes**: nbc still uses standard exit codes (0 for success, non-zero for errors) even with `--json` output. Check both the exit code and the final event type.

3. **Line-by-Line Parsing**: Each line is a complete, valid JSON object. Parse line-by-line, not as a single JSON array.

4. **Timestamps**: All timestamps are in UTC and formatted as ISO 8601 (RFC 3339).

5. **Buffering**: Output is line-buffered. Each event is flushed immediately for real-time progress monitoring.

6. **Stderr**: Errors are still written to the JSON output on stdout. Stderr may contain additional debug information when using `--verbose`.
