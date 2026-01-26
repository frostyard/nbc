# Codebase Structure

**Analysis Date:** 2026-01-26

## Directory Layout

```
nbc/
├── main.go                 # Entry point, version injection
├── cmd/                    # CLI command definitions (Cobra)
├── pkg/                    # Core business logic
│   ├── types/              # JSON output type definitions
│   ├── testutil/           # Test helpers and fixtures
│   └── dracut/             # Embedded dracut module for /etc overlay
├── docs/                   # Documentation (mkdocs source)
│   └── cli/                # CLI reference docs
├── scripts/                # Build and utility scripts
├── completions/            # Shell completion scripts
├── manpages/               # Generated man pages
├── dist/                   # Build output (goreleaser)
├── .github/                # GitHub Actions workflows
└── .planning/              # Project planning docs
    └── codebase/           # Architecture documentation
```

## Directory Purposes

**cmd/:**
- Purpose: CLI command definitions using Cobra framework
- Contains: One file per command + root.go for shared setup
- Key files:
  - `root.go`: Root command, global flags, Execute()
  - `install.go`: `nbc install` command
  - `update.go`: `nbc update` command
  - `status.go`: `nbc status` command
  - `download.go`: `nbc download` command
  - `cache.go`: `nbc cache` subcommands
  - `list.go`: `nbc list` (available disks)
  - `lint.go`: `nbc lint` (image validation)
  - `validate.go`: `nbc validate` (installation validation)
  - `interactive_install.go`: Interactive installation wizard

**pkg/:**
- Purpose: Core business logic, exported for library use
- Contains: Structs with methods, organized by domain
- Key files:
  - `install.go`: Installer type, InstallConfig, installation orchestration
  - `update.go`: SystemUpdater, A/B partition update logic
  - `container.go`: ContainerExtractor, OCI image handling
  - `bootloader.go`: BootloaderInstaller, GRUB2/systemd-boot support
  - `partition.go`: PartitionScheme, disk partitioning
  - `cache.go`: ImageCache, OCI layout caching
  - `config.go`: SystemConfig, persistent state management
  - `progress.go`: ProgressReporter, dual-mode output
  - `disk.go`: Disk detection, validation
  - `luks.go`: LUKS encryption setup
  - `dracut.go`: Initramfs regeneration
  - `etc_persistence.go`: /etc overlay mount handling
  - `lock.go`: File-based locking
  - `lint.go`: Container image linting

**pkg/types/:**
- Purpose: JSON output structures for machine-readable output
- Contains: Type definitions matching `--json` output format
- Key file: `types.go`
- Usage: Import for parsing nbc JSON output programmatically

**pkg/testutil/:**
- Purpose: Shared test utilities
- Contains: Disk simulation helpers
- Key file: `disk.go`

**pkg/dracut/95etc-overlay/:**
- Purpose: Embedded dracut module for /etc overlay persistence
- Contains: module-setup.sh, etc-overlay.sh
- Installed to target system during installation

**docs/:**
- Purpose: MkDocs documentation source
- Contains: Markdown documentation files
- Key subdir: `cli/` for CLI reference

**scripts/:**
- Purpose: Build and utility scripts
- Contains: Helper scripts for development

## Key File Locations

**Entry Points:**
- `main.go`: Application entry point
- `cmd/root.go`: CLI root command and Execute()

**Configuration:**
- `go.mod`: Go module definition and dependencies
- `.goreleaser.yaml`: Release configuration
- `Makefile`: Build targets
- `.nbc.yaml.example`: Example configuration file

**Core Logic:**
- `pkg/install.go`: Installation orchestration (920 lines)
- `pkg/update.go`: A/B update logic (1390 lines)
- `pkg/container.go`: Container extraction (711 lines)
- `pkg/bootloader.go`: Bootloader installation (1100 lines)

**Testing:**
- `pkg/*_test.go`: Unit tests co-located with source
- `pkg/integration_test.go`: Integration tests
- `test_incus*.sh`: End-to-end test scripts using Incus VMs

## Naming Conventions

**Files:**
- Go source: `snake_case.go`
- Test files: `*_test.go` (co-located with source)
- Scripts: `snake_case.sh`

**Directories:**
- All lowercase, single words preferred
- Dracut modules: `##module-name` format (e.g., `95etc-overlay`)

**Go Packages:**
- Package name matches directory name
- `pkg` is the main package (exported API)
- `types` for JSON output types
- `testutil` for test helpers

**Types:**
- Exported types: `PascalCase` (e.g., `Installer`, `SystemUpdater`)
- Config structs: `*Config` suffix (e.g., `InstallConfig`, `UpdaterConfig`)
- Result structs: `*Result` suffix (e.g., `InstallResult`)
- Option structs: `*Options` suffix (e.g., `EncryptionOptions`)

**Functions:**
- Constructors: `New*` prefix (e.g., `NewInstaller`, `NewProgressReporter`)
- Setters: `Set*` prefix (e.g., `SetVerbose`, `SetCallbacks`)
- Getters: No prefix, noun form (e.g., `GetLayoutPath`)
- Actions: Verb form (e.g., `Install`, `Update`, `Extract`)

**Constants:**
- Exported: `PascalCase` (e.g., `StagedInstallDir`, `BootloaderGRUB2`)
- Package-private: `camelCase`

## Import Organization

**Order:**
1. Standard library
2. Third-party packages
3. Internal packages (`github.com/frostyard/nbc/...`)

**Path Aliases:**
- None used; full import paths preferred
- v1 alias for go-containerregistry: `v1 "github.com/google/go-containerregistry/pkg/v1"`

## Where to Add New Code

**New Command:**
- Create: `cmd/<command>.go`
- Add to root: Register in `init()` via `rootCmd.AddCommand()`
- Pattern: Follow existing commands (install.go is comprehensive example)

**New Feature in Installation:**
- Primary code: `pkg/install.go` (in Install() method)
- If large: Extract to new file in `pkg/` (e.g., `pkg/encryption.go` -> `pkg/luks.go`)
- Tests: `pkg/<feature>_test.go`

**New JSON Output Type:**
- Add struct to: `pkg/types/types.go`
- Follow existing naming conventions (*Output, *Event)

**New System Integration:**
- Implementation: `pkg/<system>.go` (e.g., `pkg/tpm2.go`)
- Tests: `pkg/<system>_test.go`
- Pattern: Functions or struct with methods

**Utilities:**
- Shared helpers: Add to existing file or create `pkg/<domain>.go`
- Test utilities: `pkg/testutil/`

**Dracut Modules:**
- Location: `pkg/dracut/<##module-name>/`
- Installation: Add to `InstallDracutEtcOverlay()` in `pkg/dracut.go`

## Special Directories

**dist/:**
- Purpose: Goreleaser build output
- Generated: Yes (by goreleaser)
- Committed: No (gitignored)

**completions/:**
- Purpose: Shell completion scripts
- Generated: Yes (by `nbc completion`)
- Committed: Yes

**manpages/:**
- Purpose: Man page documentation
- Generated: Yes (by `nbc gendocs`)
- Committed: Yes

**.planning/:**
- Purpose: Project planning and architecture docs
- Generated: By GSD tooling
- Committed: Yes

**pkg/dracut/:**
- Purpose: Embedded dracut module files
- Generated: No (handwritten shell scripts)
- Committed: Yes
- Special: Installed to target system's /usr/lib/dracut/modules.d/

---

*Structure analysis: 2026-01-26*
