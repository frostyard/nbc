package cmd

import (
	"fmt"

	"github.com/frostyard/nbc/pkg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// ValidateOutput represents the JSON output structure for the validate command
type ValidateOutput struct {
	Device  string `json:"device"`
	Valid   bool   `json:"valid"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

var validateDevice string

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate a disk for bootc installation",
	Long:  `Validate that a disk is suitable for bootc installation.`,
	RunE:  runValidate,
}

func init() {
	rootCmd.AddCommand(validateCmd)

	validateCmd.Flags().StringVarP(&validateDevice, "device", "d", "", "Disk device to validate (required)")
	_ = validateCmd.MarkFlagRequired("device")
}

func runValidate(cmd *cobra.Command, args []string) error {
	verbose := viper.GetBool("verbose")
	jsonOutput := viper.GetBool("json")

	// Resolve device path
	device, err := pkg.GetDiskByPath(validateDevice)
	if err != nil {
		if jsonOutput {
			output := ValidateOutput{
				Device: validateDevice,
				Valid:  false,
				Error:  fmt.Sprintf("invalid device: %v", err),
			}
			return outputJSON(output)
		}
		return fmt.Errorf("invalid device: %w", err)
	}

	if verbose && !jsonOutput {
		fmt.Printf("Resolved device: %s\n", device)
	}

	// Validate disk
	minSize := uint64(10 * 1024 * 1024 * 1024) // 10 GB minimum
	if err := pkg.ValidateDisk(device, minSize); err != nil {
		if jsonOutput {
			output := ValidateOutput{
				Device: device,
				Valid:  false,
				Error:  err.Error(),
			}
			return outputJSON(output)
		}
		fmt.Printf("❌ Validation failed: %v\n", err)
		return err
	}

	if jsonOutput {
		output := ValidateOutput{
			Device:  device,
			Valid:   true,
			Message: "Device is valid for bootc installation",
		}
		return outputJSON(output)
	}

	fmt.Printf("✓ Device %s is valid for bootc installation\n", device)
	return nil
}
