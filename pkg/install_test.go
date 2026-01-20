package pkg

import (
	"context"
	"testing"
)

func TestInstallConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  InstallConfig
		wantErr string
	}{
		{
			name: "valid config with image and device",
			config: InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Device:   "/dev/sda",
			},
			wantErr: "",
		},
		{
			name: "valid config with local image and device",
			config: InstallConfig{
				LocalImage: &LocalImageSource{
					LayoutPath: "/tmp/layout",
				},
				Device: "/dev/sda",
			},
			wantErr: "",
		},
		{
			name: "valid config with image and loopback",
			config: InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Loopback: &LoopbackOptions{
					ImagePath: "/tmp/disk.img",
					SizeGB:    40,
				},
			},
			wantErr: "",
		},
		{
			name: "valid config with encryption",
			config: InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Device:   "/dev/sda",
				Encryption: &EncryptionOptions{
					Passphrase: "secret",
				},
			},
			wantErr: "",
		},
		{
			name: "valid config with encryption and TPM2",
			config: InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Device:   "/dev/sda",
				Encryption: &EncryptionOptions{
					Passphrase: "secret",
					TPM2:       true,
				},
			},
			wantErr: "",
		},
		{
			name:    "missing image and local image",
			config:  InstallConfig{Device: "/dev/sda"},
			wantErr: "either ImageRef or LocalImage is required",
		},
		{
			name:    "missing device and loopback",
			config:  InstallConfig{ImageRef: "quay.io/example/image:latest"},
			wantErr: "either Device or Loopback is required",
		},
		{
			name: "image and local image both set",
			config: InstallConfig{
				ImageRef:   "quay.io/example/image:latest",
				LocalImage: &LocalImageSource{LayoutPath: "/tmp/layout"},
				Device:     "/dev/sda",
			},
			wantErr: "imageRef and localImage are mutually exclusive",
		},
		{
			name: "device and loopback both set",
			config: InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Device:   "/dev/sda",
				Loopback: &LoopbackOptions{ImagePath: "/tmp/disk.img"},
			},
			wantErr: "device and loopback are mutually exclusive",
		},
		{
			name: "invalid filesystem type",
			config: InstallConfig{
				ImageRef:       "quay.io/example/image:latest",
				Device:         "/dev/sda",
				FilesystemType: "xfs",
			},
			wantErr: "unsupported filesystem type: xfs",
		},
		{
			name: "encryption without passphrase",
			config: InstallConfig{
				ImageRef:   "quay.io/example/image:latest",
				Device:     "/dev/sda",
				Encryption: &EncryptionOptions{},
			},
			wantErr: "encryption passphrase is required",
		},
		{
			name: "loopback without image path",
			config: InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Loopback: &LoopbackOptions{},
			},
			wantErr: "loopback ImagePath is required",
		},
		{
			name: "loopback size too small",
			config: InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Loopback: &LoopbackOptions{
					ImagePath: "/tmp/disk.img",
					SizeGB:    10, // Below minimum
				},
			},
			wantErr: "loopback size must be at least",
		},
		{
			name: "local image without layout path",
			config: InstallConfig{
				LocalImage: &LocalImageSource{},
				Device:     "/dev/sda",
			},
			wantErr: "LocalImage.LayoutPath is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.wantErr)
				} else if !containsStr(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %q, want error containing %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

// containsStr checks if s contains substr (case-sensitive)
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestNewInstaller(t *testing.T) {
	tests := []struct {
		name    string
		config  *InstallConfig
		wantErr bool
	}{
		{
			name:    "nil config returns error",
			config:  nil,
			wantErr: true,
		},
		{
			name: "valid config creates installer",
			config: &InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Device:   "/dev/sda",
			},
			wantErr: false,
		},
		{
			name: "applies default filesystem type",
			config: &InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Device:   "/dev/sda",
			},
			wantErr: false,
		},
		{
			name: "applies default loopback size",
			config: &InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Loopback: &LoopbackOptions{
					ImagePath: "/tmp/disk.img",
					// SizeGB: 0, // Should default to DefaultLoopbackSizeGB
				},
			},
			wantErr: false,
		},
		{
			name: "invalid config returns error",
			config: &InstallConfig{
				// Missing both ImageRef and LocalImage
				Device: "/dev/sda",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installer, err := NewInstaller(tt.config)
			if tt.wantErr {
				if err == nil {
					t.Error("NewInstaller() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("NewInstaller() unexpected error: %v", err)
				}
				if installer == nil {
					t.Error("NewInstaller() returned nil installer")
				}
			}
		})
	}
}

func TestNewInstaller_DefaultValues(t *testing.T) {
	t.Run("applies default filesystem type", func(t *testing.T) {
		cfg := &InstallConfig{
			ImageRef: "quay.io/example/image:latest",
			Device:   "/dev/sda",
		}
		installer, err := NewInstaller(cfg)
		if err != nil {
			t.Fatalf("NewInstaller() error: %v", err)
		}
		if installer.config.FilesystemType != "btrfs" {
			t.Errorf("FilesystemType = %q, want %q", installer.config.FilesystemType, "btrfs")
		}
	})

	t.Run("applies default mount point", func(t *testing.T) {
		cfg := &InstallConfig{
			ImageRef: "quay.io/example/image:latest",
			Device:   "/dev/sda",
		}
		installer, err := NewInstaller(cfg)
		if err != nil {
			t.Fatalf("NewInstaller() error: %v", err)
		}
		if installer.config.MountPoint != "/tmp/nbc-install" {
			t.Errorf("MountPoint = %q, want %q", installer.config.MountPoint, "/tmp/nbc-install")
		}
	})

	t.Run("applies default loopback size", func(t *testing.T) {
		cfg := &InstallConfig{
			ImageRef: "quay.io/example/image:latest",
			Loopback: &LoopbackOptions{
				ImagePath: "/tmp/disk.img",
			},
		}
		installer, err := NewInstaller(cfg)
		if err != nil {
			t.Fatalf("NewInstaller() error: %v", err)
		}
		if installer.config.Loopback.SizeGB != DefaultLoopbackSizeGB {
			t.Errorf("Loopback.SizeGB = %d, want %d", installer.config.Loopback.SizeGB, DefaultLoopbackSizeGB)
		}
	})
}

func TestInstaller_SetCallbacks(t *testing.T) {
	cfg := &InstallConfig{
		ImageRef: "quay.io/example/image:latest",
		Device:   "/dev/sda",
	}
	installer, err := NewInstaller(cfg)
	if err != nil {
		t.Fatalf("NewInstaller() error: %v", err)
	}

	// Initially callbacks should be nil
	if installer.callbacks != nil {
		t.Error("callbacks should initially be nil")
	}

	// Set callbacks
	callbacks := &InstallCallbacks{
		OnStep: func(step, total int, name string) {
			// callback placeholder
		},
	}
	installer.SetCallbacks(callbacks)

	if installer.callbacks == nil {
		t.Error("callbacks should not be nil after SetCallbacks")
	}
	if installer.progressAdapter == nil {
		t.Error("progressAdapter should not be nil after SetCallbacks")
	}
}

func TestInstaller_CallOnError(t *testing.T) {
	cfg := &InstallConfig{
		ImageRef: "quay.io/example/image:latest",
		Device:   "/dev/sda",
	}
	installer, err := NewInstaller(cfg)
	if err != nil {
		t.Fatalf("NewInstaller() error: %v", err)
	}

	t.Run("nil callbacks does not panic", func(t *testing.T) {
		// Should not panic with nil callbacks
		installer.callOnError(context.Canceled, "test")
	})

	t.Run("nil OnError does not panic", func(t *testing.T) {
		installer.SetCallbacks(&InstallCallbacks{})
		installer.callOnError(context.Canceled, "test")
	})

	t.Run("OnError is called", func(t *testing.T) {
		var calledWith error
		var calledMessage string
		installer.SetCallbacks(&InstallCallbacks{
			OnError: func(err error, msg string) {
				calledWith = err
				calledMessage = msg
			},
		})

		testErr := context.Canceled
		installer.callOnError(testErr, "test message")

		if calledWith != testErr {
			t.Errorf("OnError called with %v, want %v", calledWith, testErr)
		}
		if calledMessage != "test message" {
			t.Errorf("OnError called with message %q, want %q", calledMessage, "test message")
		}
	})
}

func TestCreateCLICallbacks(t *testing.T) {
	t.Run("text output callbacks", func(t *testing.T) {
		callbacks := CreateCLICallbacks(false)
		if callbacks == nil {
			t.Fatal("CreateCLICallbacks(false) returned nil")
		}
		if callbacks.OnStep == nil {
			t.Error("OnStep should not be nil")
		}
		if callbacks.OnMessage == nil {
			t.Error("OnMessage should not be nil")
		}
		if callbacks.OnWarning == nil {
			t.Error("OnWarning should not be nil")
		}
		if callbacks.OnError == nil {
			t.Error("OnError should not be nil")
		}
	})

	t.Run("json output callbacks", func(t *testing.T) {
		callbacks := CreateCLICallbacks(true)
		if callbacks == nil {
			t.Fatal("CreateCLICallbacks(true) returned nil")
		}
		if callbacks.OnStep == nil {
			t.Error("OnStep should not be nil")
		}
		if callbacks.OnMessage == nil {
			t.Error("OnMessage should not be nil")
		}
		if callbacks.OnWarning == nil {
			t.Error("OnWarning should not be nil")
		}
		if callbacks.OnError == nil {
			t.Error("OnError should not be nil")
		}
	})
}

func TestCallbackProgressAdapter(t *testing.T) {
	t.Run("Step calls OnStep", func(t *testing.T) {
		var stepNum, totalSteps int
		var stepName string

		adapter := newCallbackProgressAdapter(&InstallCallbacks{
			OnStep: func(step, total int, name string) {
				stepNum = step
				totalSteps = total
				stepName = name
			},
		}, 6)

		adapter.Step(3, "Testing")

		if stepNum != 3 {
			t.Errorf("step = %d, want 3", stepNum)
		}
		if totalSteps != 6 {
			t.Errorf("totalSteps = %d, want 6", totalSteps)
		}
		if stepName != "Testing" {
			t.Errorf("stepName = %q, want %q", stepName, "Testing")
		}
	})

	t.Run("Message calls OnMessage", func(t *testing.T) {
		var message string
		adapter := newCallbackProgressAdapter(&InstallCallbacks{
			OnMessage: func(msg string) {
				message = msg
			},
		}, 6)

		adapter.Message("Hello %s", "World")

		if message != "Hello World" {
			t.Errorf("message = %q, want %q", message, "Hello World")
		}
	})

	t.Run("Warning calls OnWarning", func(t *testing.T) {
		var warning string
		adapter := newCallbackProgressAdapter(&InstallCallbacks{
			OnWarning: func(msg string) {
				warning = msg
			},
		}, 6)

		adapter.Warning("Warning: %d issues", 5)

		if warning != "Warning: 5 issues" {
			t.Errorf("warning = %q, want %q", warning, "Warning: 5 issues")
		}
	})

	t.Run("Progress calls OnProgress", func(t *testing.T) {
		var percent int
		var message string
		adapter := newCallbackProgressAdapter(&InstallCallbacks{
			OnProgress: func(p int, msg string) {
				percent = p
				message = msg
			},
		}, 6)

		adapter.Progress(75, "Processing...")

		if percent != 75 {
			t.Errorf("percent = %d, want 75", percent)
		}
		if message != "Processing..." {
			t.Errorf("message = %q, want %q", message, "Processing...")
		}
	})

	t.Run("nil callbacks do not panic", func(t *testing.T) {
		adapter := newCallbackProgressAdapter(nil, 6)
		// These should not panic
		adapter.Step(1, "Test")
		adapter.Message("Test")
		adapter.Warning("Test")
		adapter.Progress(50, "Test")
		adapter.Error(context.Canceled, "Test")
	})
}
