package cmd

import (
	"testing"

	"github.com/frostyard/nbc/pkg"
)

func TestInstallNeedsConfirmation(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *pkg.InstallConfig
		force      bool
		dryRun     bool
		jsonOutput bool
		want       bool
	}{
		{
			name: "physical device without force",
			cfg: &pkg.InstallConfig{
				Device: "/dev/sda",
			},
			want: true,
		},
		{
			name: "force skips confirmation",
			cfg: &pkg.InstallConfig{
				Device: "/dev/sda",
			},
			force: true,
			want:  false,
		},
		{
			name: "yes alias skips confirmation",
			cfg: &pkg.InstallConfig{
				Device: "/dev/sda",
			},
			force: true,
			want:  false,
		},
		{
			name: "dry run skips confirmation",
			cfg: &pkg.InstallConfig{
				Device: "/dev/sda",
			},
			dryRun: true,
			want:   false,
		},
		{
			name: "json skips confirmation",
			cfg: &pkg.InstallConfig{
				Device: "/dev/sda",
			},
			jsonOutput: true,
			want:       false,
		},
		{
			name: "loopback skips confirmation",
			cfg: &pkg.InstallConfig{
				Loopback: &pkg.LoopbackOptions{
					ImagePath: "disk.img",
				},
			},
			want: false,
		},
		{
			name: "nil config skips confirmation",
			cfg:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := installNeedsConfirmation(tt.cfg, tt.force, tt.dryRun, tt.jsonOutput)
			if got != tt.want {
				t.Errorf("installNeedsConfirmation() = %v, want %v", got, tt.want)
			}
		})
	}
}
