package pkg

import (
	"testing"
)

func TestParseSizeGB(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{
			name:    "plain number at minimum",
			input:   "35",
			want:    35,
			wantErr: false,
		},
		{
			name:    "plain number above minimum",
			input:   "50",
			want:    50,
			wantErr: false,
		},
		{
			name:    "with G suffix",
			input:   "50G",
			want:    50,
			wantErr: false,
		},
		{
			name:    "with GB suffix",
			input:   "100GB",
			want:    100,
			wantErr: false,
		},
		{
			name:    "with lowercase gb suffix",
			input:   "75gb",
			want:    75,
			wantErr: false,
		},
		{
			name:    "with whitespace",
			input:   "  50  ",
			want:    50,
			wantErr: false,
		},
		{
			name:    "empty string returns default",
			input:   "",
			want:    DefaultLoopbackSizeGB,
			wantErr: false,
		},
		{
			name:    "below minimum",
			input:   "34",
			want:    0,
			wantErr: true,
		},
		{
			name:    "way below minimum",
			input:   "10",
			want:    0,
			wantErr: true,
		},
		{
			name:    "negative number",
			input:   "-50",
			want:    0,
			wantErr: true,
		},
		{
			name:    "invalid string",
			input:   "abc",
			want:    0,
			wantErr: true,
		},
		{
			name:    "float value",
			input:   "50.5",
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSizeGB(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSizeGB(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseSizeGB(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestLoopbackConstants(t *testing.T) {
	// Verify constants match expected values
	if MinLoopbackSizeGB != 35 {
		t.Errorf("MinLoopbackSizeGB = %d, want 35", MinLoopbackSizeGB)
	}
	if DefaultLoopbackSizeGB != 35 {
		t.Errorf("DefaultLoopbackSizeGB = %d, want 35", DefaultLoopbackSizeGB)
	}
}
