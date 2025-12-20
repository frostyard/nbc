package pkg

import (
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
