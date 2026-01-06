package pkg

import (
	"os"
	"strings"
	"testing"
)

func TestFormatSize(t *testing.T) {
	tests := []struct {
		name string
		size uint64
		want string
	}{
		{
			name: "bytes",
			size: 512,
			want: "512 B",
		},
		{
			name: "kilobytes",
			size: 1024,
			want: "1.0 KB",
		},
		{
			name: "megabytes",
			size: 1024 * 1024,
			want: "1.0 MB",
		},
		{
			name: "gigabytes",
			size: 1024 * 1024 * 1024,
			want: "1.0 GB",
		},
		{
			name: "terabytes",
			size: 1024 * 1024 * 1024 * 1024,
			want: "1.0 TB",
		},
		{
			name: "fractional GB",
			size: 256 * 1024 * 1024 * 1024,
			want: "256.0 GB",
		},
		{
			name: "fractional with decimals",
			size: 1536 * 1024 * 1024, // 1.5 GB
			want: "1.5 GB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatSize(tt.size)
			if got != tt.want {
				t.Errorf("FormatSize() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateDisk(t *testing.T) {
	tests := []struct {
		name    string
		device  string
		minSize uint64
		wantErr bool
		skipMsg string
	}{
		{
			name:    "invalid device path",
			device:  "/dev/nonexistent",
			minSize: 10 * 1024 * 1024 * 1024, // 10GB
			wantErr: true,
		},
		{
			name:    "empty device",
			device:  "",
			minSize: 10 * 1024 * 1024 * 1024,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipMsg != "" {
				t.Skip(tt.skipMsg)
			}
			err := ValidateDisk(tt.device, tt.minSize)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDisk() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetDiskByPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
		skip    bool              // Skip if device doesn't exist
		check   func(string) bool // Optional validation function
	}{
		{
			name:    "simple device",
			path:    "/dev/sda",
			wantErr: false,
			skip:    true, // Skip since /dev/sda might not exist on test system
			check: func(result string) bool {
				return strings.HasPrefix(result, "/dev/")
			},
		},
		{
			name:    "nvme device",
			path:    "/dev/nvme0n1",
			wantErr: false,
			skip:    true, // Skip since /dev/nvme0n1 might not exist on test system
			check: func(result string) bool {
				return strings.HasPrefix(result, "/dev/nvme")
			},
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
			skip:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skip {
				t.Skip("Skipping test that requires specific device to exist")
			}
			got, err := GetDiskByPath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetDiskByPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil && !tt.check(got) {
				t.Errorf("GetDiskByPath() = %v, validation failed", got)
			}
		})
	}
}

func TestGetDiskID(t *testing.T) {
	// This test requires /dev/disk/by-id to exist
	if _, err := os.Stat("/dev/disk/by-id"); os.IsNotExist(err) {
		t.Skip("Skipping test: /dev/disk/by-id does not exist")
	}

	// Try to get disk ID for common devices
	testDevices := []string{
		"/dev/sda",
		"/dev/nvme0n1",
		"/dev/vda",
	}

	foundOne := false
	for _, device := range testDevices {
		if _, err := os.Stat(device); os.IsNotExist(err) {
			continue
		}

		diskID, err := GetDiskID(device)
		if err != nil {
			t.Logf("Could not get disk ID for %s: %v", device, err)
			continue
		}

		foundOne = true
		t.Logf("Device: %s -> Disk ID: %s", device, diskID)

		// Verify the disk ID doesn't contain partition suffix
		if strings.Contains(diskID, "-part") {
			t.Errorf("Disk ID should not contain partition suffix: %s", diskID)
		}
	}

	if !foundOne {
		t.Skip("No testable devices found")
	}
}

func TestVerifyDiskID(t *testing.T) {
	// Test with empty disk ID (should always pass)
	match, err := VerifyDiskID("/dev/sda", "")
	if err != nil {
		t.Errorf("VerifyDiskID with empty ID failed: %v", err)
	}
	if !match {
		t.Errorf("VerifyDiskID with empty ID should return true")
	}

	// Test with a device that exists
	testDevices := []string{"/dev/sda", "/dev/nvme0n1", "/dev/vda"}
	for _, device := range testDevices {
		if _, err := os.Stat(device); os.IsNotExist(err) {
			continue
		}

		// Get the actual disk ID
		diskID, err := GetDiskID(device)
		if err != nil {
			t.Logf("Could not get disk ID for %s: %v", device, err)
			continue
		}

		// Verify it matches itself
		match, err := VerifyDiskID(device, diskID)
		if err != nil {
			t.Errorf("VerifyDiskID failed for %s: %v", device, err)
		}
		if !match {
			t.Errorf("VerifyDiskID should match for %s with its own disk ID %s", device, diskID)
		}

		// Verify it doesn't match a different ID
		fakeDiskID := "fake-disk-id-that-does-not-exist"
		match, err = VerifyDiskID(device, fakeDiskID)
		if err == nil && match {
			t.Errorf("VerifyDiskID should not match for %s with fake disk ID", device)
		}

		t.Logf("✓ VerifyDiskID works correctly for %s (disk ID: %s)", device, diskID)
		break // Only test one device
	}
}

func TestParseDeviceName(t *testing.T) {
	tests := []struct {
		name    string
		device  string
		want    string
		wantErr bool
	}{
		{
			name:    "SATA device",
			device:  "/dev/sda",
			want:    "sda",
			wantErr: false,
		},
		{
			name:    "SATA device with partition",
			device:  "/dev/sda1",
			want:    "sda",
			wantErr: false,
		},
		{
			name:    "NVMe device",
			device:  "/dev/nvme0n1",
			want:    "nvme0n1",
			wantErr: false,
		},
		{
			name:    "NVMe device with partition",
			device:  "/dev/nvme0n1p1",
			want:    "nvme0n1",
			wantErr: false,
		},
		{
			name:    "virtio device",
			device:  "/dev/vda",
			want:    "vda",
			wantErr: false,
		},
		{
			name:    "MMC device",
			device:  "/dev/mmcblk0",
			want:    "mmcblk0",
			wantErr: false,
		},
		{
			name:    "loop device",
			device:  "/dev/loop0",
			want:    "loop0",
			wantErr: false,
		},
		{
			name:    "loop device with partition",
			device:  "/dev/loop0p1",
			want:    "loop0",
			wantErr: false,
		},
		{
			name:    "loop device double digit",
			device:  "/dev/loop12",
			want:    "loop12",
			wantErr: false,
		},
		{
			name:    "unrecognized device",
			device:  "/dev/unknown123",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDeviceName(tt.device)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDeviceName(%q) error = %v, wantErr %v", tt.device, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseDeviceName(%q) = %v, want %v", tt.device, got, tt.want)
			}
		})
	}
}
