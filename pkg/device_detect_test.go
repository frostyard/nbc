package pkg

import (
	"testing"
)

func TestGetBootDeviceFromPartition(t *testing.T) {
	tests := []struct {
		name      string
		partition string
		want      string
		wantErr   bool
	}{
		{
			name:      "SATA device",
			partition: "/dev/sda3",
			want:      "/dev/sda",
			wantErr:   false,
		},
		{
			name:      "SATA device without /dev/",
			partition: "sda3",
			want:      "/dev/sda",
			wantErr:   false,
		},
		{
			name:      "NVMe device",
			partition: "/dev/nvme0n1p3",
			want:      "/dev/nvme0n1",
			wantErr:   false,
		},
		{
			name:      "NVMe device without /dev/",
			partition: "nvme0n1p3",
			want:      "/dev/nvme0n1",
			wantErr:   false,
		},
		{
			name:      "MMC device",
			partition: "/dev/mmcblk0p3",
			want:      "/dev/mmcblk0",
			wantErr:   false,
		},
		{
			name:      "virtio device",
			partition: "/dev/vda3",
			want:      "/dev/vda",
			wantErr:   false,
		},
		{
			name:      "SATA with double digit partition",
			partition: "/dev/sda12",
			want:      "/dev/sda",
			wantErr:   false,
		},
		{
			name:      "NVMe with double digit partition",
			partition: "/dev/nvme0n1p12",
			want:      "/dev/nvme0n1",
			wantErr:   false,
		},
		{
			name:      "loop device",
			partition: "/dev/loop0p3",
			want:      "/dev/loop0",
			wantErr:   false,
		},
		{
			name:      "loop device without /dev/",
			partition: "loop0p3",
			want:      "/dev/loop0",
			wantErr:   false,
		},
		{
			name:      "loop device with double digit",
			partition: "/dev/loop12p5",
			want:      "/dev/loop12",
			wantErr:   false,
		},
		{
			name:      "invalid loop device format (no partition)",
			partition: "/dev/loop0",
			want:      "",
			wantErr:   true,
		},
		{
			name:      "invalid format - no partition number",
			partition: "/dev/sda",
			want:      "/dev/sda",
			wantErr:   false, // It will strip nothing and return as-is
		},
		{
			name:      "invalid NVMe format",
			partition: "/dev/nvme0n1",
			want:      "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetBootDeviceFromPartition(tt.partition)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetBootDeviceFromPartition() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GetBootDeviceFromPartition() = %v, want %v", got, tt.want)
			}
		})
	}
}
