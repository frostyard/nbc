package cmd

import (
	"fmt"

	"github.com/frostyard/clix"
	"github.com/frostyard/nbc/pkg"
	"github.com/frostyard/nbc/pkg/types"
	"github.com/spf13/cobra"
)

type validateFlags struct {
	device string
}

var valFlags validateFlags

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate a disk for bootc installation",
	Long:  `Validate that a disk is suitable for bootc installation.`,
	RunE:  runValidate,
}

func init() {
	RootCmd.AddCommand(validateCmd)

	validateCmd.Flags().StringVarP(&valFlags.device, "device", "d", "", "Disk device to validate (required)")
	_ = validateCmd.MarkFlagRequired("device")
}

func runValidate(cmd *cobra.Command, args []string) error {
	// Resolve device path
	device, err := pkg.GetDiskByPath(valFlags.device)
	if err != nil {
		if clix.JSONOutput {
			output := types.ValidateOutput{
				Device: valFlags.device,
				Valid:  false,
				Error:  fmt.Sprintf("invalid device: %v", err),
			}
			clix.OutputJSON(output)
			return nil
		}
		return fmt.Errorf("invalid device: %w", err)
	}

	if clix.Verbose && !clix.JSONOutput {
		fmt.Printf("Resolved device: %s\n", device)
	}

	// Validate disk
	minSize := uint64(10 * 1024 * 1024 * 1024) // 10 GB minimum
	if err := pkg.ValidateDisk(device, minSize); err != nil {
		if clix.JSONOutput {
			output := types.ValidateOutput{
				Device: device,
				Valid:  false,
				Error:  err.Error(),
			}
			clix.OutputJSON(output)
			return nil
		}
		fmt.Printf("❌ Validation failed: %v\n", err)
		return err
	}

	if clix.JSONOutput {
		output := types.ValidateOutput{
			Device:  device,
			Valid:   true,
			Message: "Device is valid for bootc installation",
		}
		clix.OutputJSON(output)
		return nil
	}

	fmt.Printf("✓ Device %s is valid for bootc installation\n", device)
	return nil
}
