package pkg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"golang.org/x/term"
)

const (
	// StagedInstallDir is the directory for pre-staged installation images (e.g., on ISO)
	StagedInstallDir = "/var/cache/nbc/staged-install"
	// StagedUpdateDir is the directory for pre-downloaded update images
	StagedUpdateDir = "/var/cache/nbc/staged-update"
	// MetadataFileName is the name of the metadata file in each cached image directory
	MetadataFileName = "metadata.json"
)

// CachedImageMetadata contains metadata about a cached container image
type CachedImageMetadata struct {
	ImageRef            string            `json:"image_ref"`              // Original image reference
	ImageDigest         string            `json:"image_digest"`           // Image manifest digest (sha256:...)
	DownloadDate        string            `json:"download_date"`          // When the image was downloaded
	Architecture        string            `json:"architecture"`           // Image architecture (amd64, arm64, etc.)
	Labels              map[string]string `json:"labels,omitempty"`       // Container image labels
	OSReleasePrettyName string            `json:"os_release_pretty_name"` // PRETTY_NAME from os-release
	OSReleaseVersionID  string            `json:"os_release_version_id"`  // VERSION_ID from os-release
	OSReleaseID         string            `json:"os_release_id"`          // ID from os-release (e.g., debian, fedora)
	SizeBytes           int64             `json:"size_bytes"`             // Total uncompressed size in bytes
}

// ImageCache manages cached container images in OCI layout format
type ImageCache struct {
	CacheDir string
	Verbose  bool
}

// NewImageCache creates a new ImageCache for the specified directory
func NewImageCache(cacheDir string) *ImageCache {
	return &ImageCache{
		CacheDir: cacheDir,
	}
}

// NewStagedInstallCache creates an ImageCache for staged installation images
func NewStagedInstallCache() *ImageCache {
	return NewImageCache(StagedInstallDir)
}

// NewStagedUpdateCache creates an ImageCache for staged update images
func NewStagedUpdateCache() *ImageCache {
	return NewImageCache(StagedUpdateDir)
}

// SetVerbose enables verbose output
func (c *ImageCache) SetVerbose(verbose bool) {
	c.Verbose = verbose
}

// digestToDir converts a digest (sha256:abc123...) to a directory name (sha256-abc123...)
func digestToDir(digest string) string {
	return strings.ReplaceAll(digest, ":", "-")
}

// dirToDigest converts a directory name (sha256-abc123...) back to a digest (sha256:abc123...)
func dirToDigest(dir string) string {
	// Only replace the first occurrence to handle edge cases
	return strings.Replace(dir, "-", ":", 1)
}

// GetLayoutPath returns the full path to an image's OCI layout directory given its digest
func (c *ImageCache) GetLayoutPath(digest string) string {
	return filepath.Join(c.CacheDir, digestToDir(digest))
}

// Download pulls a container image and saves it to the cache in OCI layout format
func (c *ImageCache) Download(imageRef string) (*CachedImageMetadata, error) {
	// Parse image reference
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image reference: %w", err)
	}

	// Check if TTY is attached for spinner
	isTTY := term.IsTerminal(int(os.Stdout.Fd()))

	// Start spinner if TTY
	var stopSpinner chan struct{}
	if isTTY {
		stopSpinner = make(chan struct{})
		go func() {
			spinChars := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
			i := 0
			for {
				select {
				case <-stopSpinner:
					fmt.Print("\r\033[K") // Clear line
					return
				default:
					fmt.Printf("\r%c Downloading image...", spinChars[i%len(spinChars)])
					i++
					time.Sleep(100 * time.Millisecond)
				}
			}
		}()
	} else {
		fmt.Println("Downloading image...")
	}

	// Pull image from registry
	img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		if stopSpinner != nil {
			close(stopSpinner)
		}
		return nil, fmt.Errorf("failed to pull image: %w", err)
	}

	// Get image digest
	digest, err := img.Digest()
	if err != nil {
		if stopSpinner != nil {
			close(stopSpinner)
		}
		return nil, fmt.Errorf("failed to get image digest: %w", err)
	}

	// Stop spinner
	if stopSpinner != nil {
		close(stopSpinner)
	}

	digestStr := digest.String()
	imageDir := filepath.Join(c.CacheDir, digestToDir(digestStr))

	// Check if already cached
	if _, err := os.Stat(imageDir); err == nil {
		fmt.Printf("Image already cached: %s\n", digestStr)
		return c.readMetadata(imageDir)
	}

	fmt.Printf("Saving image to cache: %s\n", digestStr)

	// Create cache directory
	if err := os.MkdirAll(imageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Write OCI layout
	layoutPath, err := layout.Write(imageDir, nil)
	if err != nil {
		_ = os.RemoveAll(imageDir)
		return nil, fmt.Errorf("failed to create OCI layout: %w", err)
	}

	// Append image to layout
	if err := layoutPath.AppendImage(img); err != nil {
		_ = os.RemoveAll(imageDir)
		return nil, fmt.Errorf("failed to write image to layout: %w", err)
	}

	// Extract metadata from image
	metadata, err := c.extractMetadata(img, imageRef, digestStr)
	if err != nil {
		_ = os.RemoveAll(imageDir)
		return nil, fmt.Errorf("failed to extract metadata: %w", err)
	}

	// Write metadata file
	if err := c.writeMetadata(imageDir, metadata); err != nil {
		_ = os.RemoveAll(imageDir)
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	fmt.Printf("Image cached successfully: %s\n", digestStr)
	return metadata, nil
}

// extractMetadata extracts metadata from a container image
func (c *ImageCache) extractMetadata(img v1.Image, imageRef, digestStr string) (*CachedImageMetadata, error) {
	// Get image config
	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("failed to get image config: %w", err)
	}

	// Calculate total size from layers
	var totalSize int64
	layers, err := img.Layers()
	if err == nil {
		for _, layer := range layers {
			size, _ := layer.Size()
			totalSize += size
		}
	}

	metadata := &CachedImageMetadata{
		ImageRef:     imageRef,
		ImageDigest:  digestStr,
		DownloadDate: time.Now().UTC().Format(time.RFC3339),
		Architecture: cfg.Architecture,
		Labels:       cfg.Config.Labels,
		SizeBytes:    totalSize,
	}

	// Try to extract os-release info from labels (common convention)
	if metadata.Labels != nil {
		if prettyName, ok := metadata.Labels["org.opencontainers.image.title"]; ok {
			metadata.OSReleasePrettyName = prettyName
		}
		if version, ok := metadata.Labels["org.opencontainers.image.version"]; ok {
			metadata.OSReleaseVersionID = version
		}
	}

	return metadata, nil
}

// writeMetadata writes metadata to a JSON file in the image directory
func (c *ImageCache) writeMetadata(imageDir string, metadata *CachedImageMetadata) error {
	metadataPath := filepath.Join(imageDir, MetadataFileName)
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}
	return os.WriteFile(metadataPath, data, 0644)
}

// readMetadata reads metadata from a JSON file in the image directory
func (c *ImageCache) readMetadata(imageDir string) (*CachedImageMetadata, error) {
	metadataPath := filepath.Join(imageDir, MetadataFileName)
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	var metadata CachedImageMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}
	return &metadata, nil
}

// GetImage loads an image from the cache by digest or image reference
func (c *ImageCache) GetImage(digestOrRef string) (v1.Image, *CachedImageMetadata, error) {
	// Try to find by digest first
	imageDir := filepath.Join(c.CacheDir, digestToDir(digestOrRef))
	if _, err := os.Stat(imageDir); err == nil {
		return c.loadFromDir(imageDir)
	}

	// Try to find by digest prefix
	entries, err := os.ReadDir(c.CacheDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read cache directory: %w", err)
	}

	var matches []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Check if this entry matches the digest prefix
		if strings.HasPrefix(entry.Name(), digestToDir(digestOrRef)) {
			matches = append(matches, entry.Name())
		}
		// Also check by image reference in metadata
		metadata, err := c.readMetadata(filepath.Join(c.CacheDir, entry.Name()))
		if err == nil && metadata.ImageRef == digestOrRef {
			matches = append(matches, entry.Name())
		}
	}

	if len(matches) == 0 {
		return nil, nil, fmt.Errorf("image not found in cache: %s", digestOrRef)
	}
	if len(matches) > 1 {
		return nil, nil, fmt.Errorf("ambiguous reference, multiple matches found: %v", matches)
	}

	imageDir = filepath.Join(c.CacheDir, matches[0])
	return c.loadFromDir(imageDir)
}

// loadFromDir loads an image from an OCI layout directory
func (c *ImageCache) loadFromDir(imageDir string) (v1.Image, *CachedImageMetadata, error) {
	// Read metadata
	metadata, err := c.readMetadata(imageDir)
	if err != nil {
		return nil, nil, err
	}

	// Open OCI layout
	layoutPath, err := layout.FromPath(imageDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open OCI layout: %w", err)
	}

	// Get image index
	idx, err := layoutPath.ImageIndex()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get image index: %w", err)
	}

	// Get manifest list
	manifest, err := idx.IndexManifest()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get index manifest: %w", err)
	}

	if len(manifest.Manifests) == 0 {
		return nil, nil, fmt.Errorf("no images found in layout")
	}

	// Load the first image
	img, err := layoutPath.Image(manifest.Manifests[0].Digest)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load image from layout: %w", err)
	}

	return img, metadata, nil
}

// List returns all cached images with their metadata
func (c *ImageCache) List() ([]CachedImageMetadata, error) {
	var images []CachedImageMetadata

	if _, err := os.Stat(c.CacheDir); os.IsNotExist(err) {
		return images, nil // Empty list if directory doesn't exist
	}

	entries, err := os.ReadDir(c.CacheDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		metadata, err := c.readMetadata(filepath.Join(c.CacheDir, entry.Name()))
		if err != nil {
			if c.Verbose {
				fmt.Printf("Warning: skipping %s: %v\n", entry.Name(), err)
			}
			continue
		}
		images = append(images, *metadata)
	}

	return images, nil
}

// IsCached checks if an image is in the cache by digest
func (c *ImageCache) IsCached(digest string) (bool, error) {
	imageDir := filepath.Join(c.CacheDir, digestToDir(digest))
	_, err := os.Stat(imageDir)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// Remove removes a cached image by digest or digest prefix
func (c *ImageCache) Remove(digestOrPrefix string) error {
	// Find matching directories
	entries, err := os.ReadDir(c.CacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("cache directory does not exist")
		}
		return fmt.Errorf("failed to read cache directory: %w", err)
	}

	var matches []string
	prefix := digestToDir(digestOrPrefix)
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			matches = append(matches, entry.Name())
		}
	}

	if len(matches) == 0 {
		return fmt.Errorf("no cached image matches: %s", digestOrPrefix)
	}
	if len(matches) > 1 {
		return fmt.Errorf("ambiguous prefix, multiple matches: %v", matches)
	}

	imageDir := filepath.Join(c.CacheDir, matches[0])
	if err := os.RemoveAll(imageDir); err != nil {
		return fmt.Errorf("failed to remove cached image: %w", err)
	}

	fmt.Printf("Removed cached image: %s\n", dirToDigest(matches[0]))
	return nil
}

// Clear removes all cached images
func (c *ImageCache) Clear() error {
	if _, err := os.Stat(c.CacheDir); os.IsNotExist(err) {
		return nil // Nothing to clear
	}

	entries, err := os.ReadDir(c.CacheDir)
	if err != nil {
		return fmt.Errorf("failed to read cache directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			path := filepath.Join(c.CacheDir, entry.Name())
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("failed to remove %s: %w", entry.Name(), err)
			}
		}
	}

	fmt.Printf("Cleared cache: %s\n", c.CacheDir)
	return nil
}

// GetSingle returns the single cached image (for staged-update where only one is expected)
// Returns nil if no image is cached, error if multiple images exist
func (c *ImageCache) GetSingle() (*CachedImageMetadata, error) {
	images, err := c.List()
	if err != nil {
		return nil, err
	}

	if len(images) == 0 {
		return nil, nil
	}
	if len(images) > 1 {
		return nil, fmt.Errorf("multiple images found in cache, expected at most one")
	}

	return &images[0], nil
}

// GetImageByPath loads an image from an OCI layout directory path
func GetImageByPath(layoutPath string) (v1.Image, *CachedImageMetadata, error) {
	cache := &ImageCache{}
	return cache.loadFromDir(layoutPath)
}
