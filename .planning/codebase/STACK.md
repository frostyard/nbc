# Technology Stack

**Analysis Date:** 2026-01-26

## Languages

**Primary:**
- Go 1.24.2 - All application code

**Secondary:**
- Bash - Build scripts (`scripts/completions.sh`, `scripts/manpages.sh`, `test_incus*.sh`)
- Shell - Dracut module scripts (`pkg/dracut/95etc-overlay/*.sh`)

## Runtime

**Environment:**
- Go 1.24+ (CGO disabled, pure Go builds)
- Linux only (amd64 target, arm64 build supported)

**Package Manager:**
- Go Modules
- Lockfile: `go.sum` present

## Frameworks

**Core:**
- `github.com/spf13/cobra` v1.10.2 - CLI framework
- `github.com/spf13/viper` v1.21.0 - Configuration management
- `github.com/charmbracelet/fang` v0.4.4 - Cobra extensions (signal handling, version)

**TUI/Interactive:**
- `github.com/charmbracelet/huh` v0.8.0 - Interactive prompts/forms
- `github.com/charmbracelet/lipgloss` v1.1.0 - Terminal styling
- `github.com/muesli/termenv` v0.16.0 - Terminal detection

**Container:**
- `github.com/google/go-containerregistry` v0.20.7 - OCI image handling
- `github.com/docker/docker` v28.5.2 - Docker daemon client

**Testing:**
- Standard library `testing` package
- No external test framework

**Build/Release:**
- GoReleaser Pro v2 - Cross-platform builds and packaging

## Key Dependencies

**Critical:**
- `github.com/google/go-containerregistry` - Core OCI image pull/extract functionality
- `github.com/spf13/cobra` - CLI command structure
- `golang.org/x/term` v0.39.0 - Terminal mode detection for interactive prompts

**Infrastructure:**
- `github.com/docker/docker` - Local daemon image access (podman/docker)
- `github.com/charmbracelet/huh` - Interactive installation wizard
- `github.com/charmbracelet/lipgloss` - Styled console output

**Indirect but Important:**
- `go.opentelemetry.io/otel` - OpenTelemetry (via go-containerregistry)
- `google.golang.org/protobuf` - Protocol buffers (via container registry)

## Configuration

**Environment:**
- `.envrc` contains `GORELEASER_KEY` for GoReleaser Pro
- Runtime config via `~/.nbc.yaml` (viper-based)
- System config stored at `/var/lib/nbc/state/config.json`

**Key Runtime Config (viper flags):**
- `verbose` - Verbose output
- `dry-run` - Safe mode (no changes)
- `json` - Machine-readable JSON output

**Build Configuration:**
- `.goreleaser.yaml` - Release automation
- `.svu.yaml` - Semantic versioning
- `Makefile` - Local development

## Build Configuration

**Go Build:**
```bash
CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -o nbc .
```

**Ldflags Injected:**
- `main.version` - Semantic version
- `main.commit` - Git commit hash
- `main.date` - Build date
- `main.builtBy` - Build system (goreleaser/local)

**Docker Build:**
- Multi-stage Dockerfile using `golang:1.24-alpine` builder
- Runtime: `alpine:latest`

## Package Outputs

**Binary Formats:**
- Linux amd64 binary (primary)
- Linux arm64 binary (CI only)

**Package Formats (via GoReleaser):**
- `.deb` - Debian/Ubuntu
- `.rpm` - Fedora/RHEL/CentOS
- `.apk` - Alpine

**Package Name:** `frostyard-nbc`

**System Dependencies (declared in nfpms):**
- Required: gdisk, dosfstools, e2fsprogs, util-linux, parted, udev, rsync, coreutils, passwd, efibootmgr
- Recommended: grub-efi-amd64, systemd-boot, cryptsetup, btrfs-progs, dracut, podman
- Suggested: initramfs-tools

## Platform Requirements

**Development:**
- Go 1.24+
- Linux (for testing - requires root, loop devices)
- golangci-lint (optional, for linting)
- Incus/LXD (optional, for VM integration tests)

**Production:**
- Linux amd64
- UEFI firmware
- Root privileges
- Container runtime (podman or docker) - optional, for localhost images

---

*Stack analysis: 2026-01-26*
