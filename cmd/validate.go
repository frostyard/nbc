package cmd

import (
	"fmt"

	"github.com/frostyard/nbc/pkg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

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

	// Resolve device path
	device, err := pkg.GetDiskByPath(validateDevice)
	if err != nil {
		return fmt.Errorf("invalid device: %w", err)
	}

	if verbose {
		fmt.Printf("Resolved device: %s\n", device)
	}

	// Validate disk
	minSize := uint64(10 * 1024 * 1024 * 1024) // 10 GB minimum
	if err := pkg.ValidateDisk(device, minSize); err != nil {
		fmt.Printf("❌ Validation failed: %v\n", err)
		return err
	}

	fmt.Printf("✓ Device %s is valid for bootc installation\n", device)
	return nil
}
