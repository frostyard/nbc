package pkg

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/docker/docker/client"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// ContainerExtractor handles extracting container images to disk
type ContainerExtractor struct {
	ImageRef        string
	TargetDir       string
	Verbose         bool
	JSONOutput      bool
	LocalLayoutPath string // Path to OCI layout directory for local image
	Progress        *ProgressReporter
}

// NewContainerExtractor creates a new ContainerExtractor
func NewContainerExtractor(imageRef, targetDir string) *ContainerExtractor {
	return &ContainerExtractor{
		ImageRef:  imageRef,
		TargetDir: targetDir,
		Progress:  NewProgressReporter(false, 1),
	}
}

// NewContainerExtractorFromLocal creates a ContainerExtractor for a local OCI layout
func NewContainerExtractorFromLocal(layoutPath, targetDir string) *ContainerExtractor {
	return &ContainerExtractor{
		LocalLayoutPath: layoutPath,
		TargetDir:       targetDir,
		Progress:        NewProgressReporter(false, 1),
	}
}

// SetVerbose enables verbose output
func (c *ContainerExtractor) SetVerbose(verbose bool) {
	c.Verbose = verbose
}

// SetJSONOutput enables JSON output mode
func (c *ContainerExtractor) SetJSONOutput(jsonOutput bool) {
	c.JSONOutput = jsonOutput
	c.Progress = NewProgressReporter(jsonOutput, 1)
}

// SetProgress sets the progress reporter directly
func (c *ContainerExtractor) SetProgress(p *ProgressReporter) {
	c.Progress = p
}

// Extract extracts the container filesystem to the target directory using go-containerregistry
func (c *ContainerExtractor) Extract(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	var img v1.Image
	var err error

	// Load image from local OCI layout or pull from registry
	if c.LocalLayoutPath != "" {
		c.Progress.MessagePlain("Extracting container image from local cache...")
		img, err = LoadImageFromOCILayout(c.LocalLayoutPath)
		if err != nil {
			return fmt.Errorf("failed to load image from local cache: %w", err)
		}
	} else {
		c.Progress.MessagePlain("Extracting container image %s...", c.ImageRef)

		// Parse image reference
		ref, err := name.ParseReference(c.ImageRef)
		if err != nil {
			return fmt.Errorf("failed to parse image reference: %w", err)
		}

		// For localhost images, try local daemon first (podman/docker)
		if strings.HasPrefix(c.ImageRef, "localhost/") {
			c.Progress.Message("Checking local daemon...")

			// Try podman CLI first for localhost images (more reliable than daemon API)
			// Check if podman is available and image exists
			checkCmd := exec.CommandContext(ctx, "podman", "image", "exists", c.ImageRef)
			if checkCmd.Run() == nil {
				// Image exists in podman, save it to OCI layout directory for extraction
				c.Progress.Message("Found image in podman, using local copy")
				tmpLayout := filepath.Join(os.TempDir(), fmt.Sprintf("nbc-oci-%d", os.Getpid()))
				defer func() {
					if err := os.RemoveAll(tmpLayout); err != nil {
						c.Progress.Warning("failed to remove temporary OCI layout: %v", err)
					}
				}()

				// Create the layout directory
				if err := os.MkdirAll(tmpLayout, 0755); err == nil {
					saveCmd := exec.CommandContext(ctx, "podman", "image", "save", "--format=oci-dir", "-o", tmpLayout, c.ImageRef)
					if err := saveCmd.Run(); err == nil {
						// Load from the OCI layout directory
						img, err = LoadImageFromOCILayout(tmpLayout)
						if err == nil {
							c.Progress.Message("Successfully loaded image from podman")
						}
					}
				}
			}

			// If podman approach failed, try daemon API with both sockets
			if img == nil {
				sockets := []string{
					"unix:///var/run/podman/podman.sock",
					"unix:///var/run/docker.sock",
				}

				for _, sock := range sockets {
					cli, err := client.NewClientWithOpts(client.WithHost(sock), client.WithAPIVersionNegotiation())
					if err != nil {
						continue
					}

					// Try to get the image using this client
					img, err = daemon.Image(ref, daemon.WithClient(cli))
					if err == nil {
						c.Progress.Message("Using image from local daemon")
						break
					}
					_ = cli.Close()
				}
			}

			if img == nil && c.Verbose {
				c.Progress.Message("Local lookup failed, will try registry")
			}
		}

		// If not found locally or not a localhost image, pull from registry
		if img == nil {
			c.Progress.Message("Pulling image...")
			img, err = remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
			if err != nil {
				return fmt.Errorf("failed to pull image: %w", err)
			}
		}
	}

	// Check for cancellation before layer extraction
	if err := ctx.Err(); err != nil {
		return err
	}

	// Get image layers
	c.Progress.Message("Extracting layers...")
	layers, err := img.Layers()
	if err != nil {
		return fmt.Errorf("failed to get image layers: %w", err)
	}

	// Extract each layer
	for i, layer := range layers {
		// Check for cancellation between layers
		if err := ctx.Err(); err != nil {
			return err
		}

		if c.Verbose {
			digest, _ := layer.Digest()
			c.Progress.Message("Extracting layer %d/%d (%s)...", i+1, len(layers), digest)
		}

		// Get layer contents as tar stream
		rc, err := layer.Uncompressed()
		if err != nil {
			return fmt.Errorf("failed to decompress layer %d: %w", i, err)
		}

		// Extract tar contents to target directory
		if err := extractTar(ctx, rc, c.TargetDir); err != nil {
			_ = rc.Close()
			return fmt.Errorf("failed to extract layer %d: %w", i, err)
		}
		if err := rc.Close(); err != nil {
			return fmt.Errorf("failed to close layer %d: %w", i, err)
		}
	}

	c.Progress.MessagePlain("Container filesystem extracted successfully")
	return nil
}

// extractTar extracts a tar stream to a target directory
func extractTar(ctx context.Context, r io.Reader, targetDir string) error {
	tr := tar.NewReader(r)
	fileCount := 0

	for {
		// Check for cancellation every 1000 files
		if fileCount%1000 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		fileCount++

		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		target := filepath.Join(targetDir, header.Name)

		// Ensure target is within targetDir (prevent path traversal)
		cleanTarget := filepath.Clean(target)
		cleanTargetDir := filepath.Clean(targetDir) + string(filepath.Separator)
		if !strings.HasPrefix(cleanTarget+string(filepath.Separator), cleanTargetDir) && cleanTarget != filepath.Clean(targetDir) {
			continue
		}

		// Handle whiteouts (deleted files in overlay filesystems)
		// Whiteouts are special files with .wh. prefix indicating deletion
		base := filepath.Base(header.Name)
		dir := filepath.Dir(header.Name)

		// Opaque whiteout: .wh..wh..opq means "delete all files in this directory"
		if base == ".wh..wh..opq" {
			// Remove all contents of the directory
			targetDir := filepath.Join(targetDir, dir)
			// if the targetDir contains "efi" just skip it to avoid deleting efi contents
			if filepath.Base(targetDir) == "efi" {
				continue
			}
			if filepath.Base(targetDir) == "boot" {
				continue
			}
			if err := os.RemoveAll(targetDir); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to clear directory for opaque whiteout %s: %w", targetDir, err)
			}
			// Recreate the directory
			if err := os.MkdirAll(targetDir, 0755); err != nil {
				return fmt.Errorf("failed to recreate directory after opaque whiteout %s: %w", targetDir, err)
			}
			continue
		}

		// Regular whiteout: .wh.filename means "delete filename"
		if len(base) > 4 && base[:4] == ".wh." {
			// The whiteout indicates we should delete the file it references
			originalName := base[4:] // Remove .wh. prefix
			whiteoutTarget := filepath.Join(targetDir, dir, originalName)
			// Remove the file/directory that this whiteout references
			if err := os.RemoveAll(whiteoutTarget); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove whiteout target %s: %w", whiteoutTarget, err)
			}
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", target, err)
			}

			// Set ownership first
			_ = os.Chown(target, header.Uid, header.Gid)

			// Set mode including special bits (SUID/SGID/sticky)
			// Convert from Unix format to Go's FileMode format
			mode := os.FileMode(header.Mode & 0777)
			if header.Mode&04000 != 0 {
				mode |= os.ModeSetuid
			}
			if header.Mode&02000 != 0 {
				mode |= os.ModeSetgid
			}
			if header.Mode&01000 != 0 {
				mode |= os.ModeSticky
			}

			if err := os.Chmod(target, mode); err != nil {
				return fmt.Errorf("failed to set mode on directory %s: %w", target, err)
			}

		case tar.TypeReg:
			// Create parent directory if it doesn't exist
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			// Create and write file with basic permissions first
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", target, err)
			}

			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return fmt.Errorf("failed to write file %s: %w", target, err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("failed to close file %s: %w", target, err)
			}

			// Set ownership first (this will clear SUID/SGID on Linux for security)
			// Ignoring error - ownership change may fail if not root or UIDs don't exist
			// We'll still try to set permissions below
			_ = os.Chown(target, header.Uid, header.Gid)

			// Set the mode AFTER ownership to restore SUID/SGID/sticky bits
			// The tar header.Mode is int64 in Unix format (low 12 bits: permissions + special bits)
			// Need to convert to Go's os.FileMode which uses different bit positions for special bits
			mode := os.FileMode(header.Mode & 0777) // rwxrwxrwx

			// Add special bits if present in tar header
			if header.Mode&04000 != 0 { // SUID in Unix format
				mode |= os.ModeSetuid
			}
			if header.Mode&02000 != 0 { // SGID in Unix format
				mode |= os.ModeSetgid
			}
			if header.Mode&01000 != 0 { // Sticky in Unix format
				mode |= os.ModeSticky
			}

			if err := os.Chmod(target, mode); err != nil {
				return fmt.Errorf("failed to set mode on file %s: %w", target, err)
			}

		case tar.TypeSymlink:
			// Remove existing file/link if it exists

			// check to see if the target is a file or directory
			info, err := os.Lstat(target)
			if err == nil {
				if info.IsDir() {
					if err := os.RemoveAll(target); err != nil {
						return fmt.Errorf("failed to remove existing directory %s: %w", target, err)
					}
				} else {
					if err := os.Remove(target); err != nil {
						return fmt.Errorf("failed to remove existing file %s: %w", target, err)
					}
				}
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("failed to stat existing file %s: %w", target, err)
			}

			// Create symlink
			if err := os.Symlink(header.Linkname, target); err != nil {
				return fmt.Errorf("failed to create symlink %s: %w", target, err)
			}
			// Set ownership on symlink (may fail without root, but that's okay)
			_ = os.Lchown(target, header.Uid, header.Gid)

		case tar.TypeLink:
			// Hard link
			linkTarget := filepath.Join(targetDir, header.Linkname)
			if err := os.Link(linkTarget, target); err != nil {
				// If hard link fails, try copying the file
				if err := copyFile(linkTarget, target); err != nil {
					return fmt.Errorf("failed to create hard link or copy %s: %w", target, err)
				}
				// For copied files, set ownership and mode
				_ = os.Chown(target, header.Uid, header.Gid)

				mode := os.FileMode(header.Mode & 0777)
				if header.Mode&04000 != 0 {
					mode |= os.ModeSetuid
				}
				if header.Mode&02000 != 0 {
					mode |= os.ModeSetgid
				}
				if header.Mode&01000 != 0 {
					mode |= os.ModeSticky
				}

				if err := os.Chmod(target, mode); err != nil {
					return fmt.Errorf("failed to set mode on copied hard link %s: %w", target, err)
				}
			}
			// Note: For actual hard links, ownership/mode are shared with the target
		}
	}

	return nil
}

// CreateFstab creates an /etc/fstab file with the proper mount points
func CreateFstab(ctx context.Context, targetDir string, scheme *PartitionScheme, progress *ProgressReporter) error {
	if progress != nil {
		progress.Message("Creating /etc/fstab...")
	}

	// Only need root2 UUID for the commented-out alternate root entry
	root2UUID, err := GetPartitionUUID(ctx, scheme.Root2Partition)
	if err != nil {
		return fmt.Errorf("failed to get root2 UUID: %w", err)
	}

	// Create fstab content
	// Note: /boot is auto-mounted by systemd-gpt-auto-generator (ESP partition type)
	// Note: /var is mounted via kernel command line (systemd.mount-extra)
	fstabContent := fmt.Sprintf(`# /etc/fstab
# Created by nbc
#
# Most mounts are handled automatically:
# - Root: specified via kernel cmdline root=UUID parameter
# - /boot: auto-mounted by systemd (ESP partition type, labeled UEFI)
# - /var: mounted via kernel cmdline systemd.mount-extra parameter
#
# This file is kept minimal and can be empty on systems with discoverable partitions.

# Second root filesystem (root2 - inactive/alternate)
# UUID=%s	/		ext4	defaults	0 1
`, root2UUID)

	// Write fstab
	fstabPath := filepath.Join(targetDir, "etc", "fstab")
	if err := os.WriteFile(fstabPath, []byte(fstabContent), 0644); err != nil {
		return fmt.Errorf("failed to write fstab: %w", err)
	}

	if progress != nil {
		progress.Message("Created /etc/fstab")
	}
	return nil
}

// SetupSystemDirectories creates necessary system directories
func SetupSystemDirectories(targetDir string, progress *ProgressReporter) error {
	progress.Message("Setting up system directories...")

	directories := []string{
		"dev",
		"proc",
		"sys",
		"run",
		"tmp",
		"var/tmp",
		"boot",
		// Note: .etc.lower is populated separately by PopulateEtcLower()
		// Common bind-mount targets - must exist on ro root for bind mounts to work
		// These may be bind-mounted from /var (e.g., /home -> /var/home)
		"mnt",
		"media",
		"opt",
		"srv",
	}

	for _, dir := range directories {
		path := filepath.Join(targetDir, dir)
		if err := os.MkdirAll(path, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Set proper permissions for tmp directories (sticky bit + 0777)
	// Note: os.Chmod with 0o1777 doesn't set sticky bit in Go; must use os.ModeSticky
	_ = os.Chmod(filepath.Join(targetDir, "tmp"), os.ModeSticky|0777)
	_ = os.Chmod(filepath.Join(targetDir, "var", "tmp"), os.ModeSticky|0777)
	progress.Message("System directories created")
	return nil
}

// PrepareMachineID ensures /etc/machine-id contains "uninitialized" for first-boot detection.
// This is required for read-only root filesystems where systemd cannot create the file at boot.
// systemd will detect "uninitialized" and properly initialize the machine-id on first boot.
func PrepareMachineID(targetDir string, progress *ProgressReporter) error {
	machineIDPath := filepath.Join(targetDir, "etc", "machine-id")

	// Check current state
	content, err := os.ReadFile(machineIDPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read machine-id: %w", err)
	}

	// If file doesn't exist, is empty, or already "uninitialized", set it correctly
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" || trimmed == "uninitialized" || os.IsNotExist(err) {
		// Remove existing file first (may be read-only 0444)
		_ = os.Remove(machineIDPath)
		if err := os.WriteFile(machineIDPath, []byte("uninitialized\n"), 0444); err != nil {
			return fmt.Errorf("failed to write machine-id: %w", err)
		}
		progress.Message("Prepared /etc/machine-id for first boot")
		return nil
	}

	// File has a real machine-id - this shouldn't happen for clean container images
	// The lint check should have caught this, but warn and continue
	progress.Warning("/etc/machine-id already has a value, leaving unchanged")
	return nil
}

// ChrootCommand runs a command in a chroot environment
func ChrootCommand(targetDir string, command string, args ...string) error {
	// Mount necessary filesystems for chroot
	mounts := [][]string{
		{"mount", "--bind", "/dev", filepath.Join(targetDir, "dev")},
		{"mount", "--bind", "/proc", filepath.Join(targetDir, "proc")},
		{"mount", "--bind", "/sys", filepath.Join(targetDir, "sys")},
		{"mount", "--bind", "/run", filepath.Join(targetDir, "run")},
	}

	for _, mount := range mounts {
		if err := exec.Command(mount[0], mount[1:]...).Run(); err != nil {
			// Continue even if mount fails (might already be mounted)
			continue
		}
	}

	// Cleanup function to unmount
	defer func() {
		_ = exec.Command("umount", filepath.Join(targetDir, "run")).Run()
		_ = exec.Command("umount", filepath.Join(targetDir, "sys")).Run()
		_ = exec.Command("umount", filepath.Join(targetDir, "proc")).Run()
		_ = exec.Command("umount", filepath.Join(targetDir, "dev")).Run()
	}()

	// Build chroot command
	chrootArgs := []string{targetDir, command}
	chrootArgs = append(chrootArgs, args...)

	cmd := exec.Command("chroot", chrootArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}

// ParseOSRelease reads and parses /etc/os-release from the target directory
// Returns PRETTY_NAME if available, otherwise NAME, otherwise ID, or "Linux" as fallback
func ParseOSRelease(targetDir string) string {
	osReleasePath := filepath.Join(targetDir, "etc", "os-release")

	// Try /etc/os-release first, then /usr/lib/os-release as fallback
	data, err := os.ReadFile(osReleasePath)
	if err != nil {
		osReleasePath = filepath.Join(targetDir, "usr", "lib", "os-release")
		data, err = os.ReadFile(osReleasePath)
		if err != nil {
			// File doesn't exist or can't be read
			return "Linux"
		}
	}

	// Parse the file for PRETTY_NAME, NAME, or ID
	lines := strings.Split(string(data), "\n")
	values := make(map[string]string)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split on first =
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove quotes if present
		value = strings.Trim(value, "\"'")

		values[key] = value
	}

	// Return in priority order: PRETTY_NAME > NAME > ID > "Linux"
	if prettyName, ok := values["PRETTY_NAME"]; ok && prettyName != "" {
		return prettyName
	}
	if name, ok := values["NAME"]; ok && name != "" {
		return name
	}
	if id, ok := values["ID"]; ok && id != "" {
		return id
	}

	return "Linux"
}

// VerifyExtraction checks that the extracted filesystem has essential directories
// and files, returning an error if the extraction appears incomplete or failed.
// This helps catch silent extraction failures before proceeding with the update.
func VerifyExtraction(targetDir string) error {
	// Critical directories that must exist in any Linux root filesystem
	requiredDirs := []string{
		"usr",
		"usr/bin",
		"usr/lib",
		"etc",
	}

	// Critical files/symlinks that must exist
	requiredPaths := []string{
		"usr/lib/os-release", // Every modern Linux has this
	}

	// Check required directories
	for _, dir := range requiredDirs {
		path := filepath.Join(targetDir, dir)
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			return fmt.Errorf("extraction verification failed: required directory %s is missing", dir)
		}
		if err != nil {
			return fmt.Errorf("extraction verification failed: cannot stat %s: %w", dir, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("extraction verification failed: %s exists but is not a directory", dir)
		}
	}

	// Check required files (follow symlinks)
	for _, file := range requiredPaths {
		path := filepath.Join(targetDir, file)
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			return fmt.Errorf("extraction verification failed: required file %s is missing", file)
		}
		if err != nil {
			return fmt.Errorf("extraction verification failed: cannot stat %s: %w", file, err)
		}
		if info.IsDir() {
			return fmt.Errorf("extraction verification failed: %s should be a file, not a directory", file)
		}
	}

	// Check minimum filesystem size (a valid Linux rootfs should be at least 100MB)
	// This catches cases where extraction started but failed silently
	var totalSize int64
	err := filepath.WalkDir(targetDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Ignore errors during size calculation
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err == nil {
				totalSize += info.Size()
			}
		}
		// Stop early if we've already confirmed sufficient content
		if totalSize > 100*1024*1024 { // 100MB
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return fmt.Errorf("extraction verification failed: error walking filesystem: %w", err)
	}

	minSize := int64(100 * 1024 * 1024) // 100MB minimum
	if totalSize < minSize {
		return fmt.Errorf("extraction verification failed: filesystem too small (%d bytes, expected at least %d bytes) - extraction may have failed", totalSize, minSize)
	}

	return nil
}

// LoadImageFromOCILayout loads a container image from an OCI layout directory
func LoadImageFromOCILayout(layoutPath string) (v1.Image, error) {
	// Open OCI layout
	p, err := layout.FromPath(layoutPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open OCI layout: %w", err)
	}

	// Get image index
	idx, err := p.ImageIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to get image index: %w", err)
	}

	// Get manifest list
	manifest, err := idx.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("failed to get index manifest: %w", err)
	}

	if len(manifest.Manifests) == 0 {
		return nil, fmt.Errorf("no images found in layout")
	}

	// Load the first image
	img, err := p.Image(manifest.Manifests[0].Digest)
	if err != nil {
		return nil, fmt.Errorf("failed to load image from layout: %w", err)
	}

	return img, nil
}
