package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	commit  string
	date    string
	cfgFile string
	builtBy string
	rootCmd = &cobra.Command{
		Use:   "nbc",
		Short: "A bootc container installer for physical disks",
		Long: `nbc is a tool for installing bootc compatible containers to physical disks.
It automates the process of preparing disks and deploying bootable container images.`,
	}
)

// SetVersion sets the version for the root command
func SetVersion(version string) {
	rootCmd.Version = version
}

func SetCommit(incoming_commit string) {
	commit = incoming_commit
}

func SetDate(incoming_date string) {
	date = incoming_date
}
func SetBuiltBy(incoming_builtBy string) {
	builtBy = incoming_builtBy
}

func makeVersionString() string {
	return fmt.Sprintf("%s (Commit: %s) (Date: %s) (Built by: %s)", rootCmd.Version, commit, date, builtBy)
}

// Execute runs the root command
func Execute() error {
	if err := fang.Execute(
		context.Background(),
		rootCmd,
		fang.WithVersion(makeVersionString()),
		fang.WithNotifySignal(os.Interrupt, os.Kill),
	); err != nil {
		return err
	}
	return nil
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.nbc.yaml)")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolP("dry-run", "n", false, "dry run mode (no actual changes)")
	rootCmd.PersistentFlags().Bool("json", false, "output in JSON format for machine-readable output")

	_ = viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	_ = viper.BindPFlag("dry-run", rootCmd.PersistentFlags().Lookup("dry-run"))
	_ = viper.BindPFlag("json", rootCmd.PersistentFlags().Lookup("json"))
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting home directory: %v\n", err)
			os.Exit(1)
		}

		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".nbc")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		if viper.GetBool("verbose") {
			fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
		}
	}
}
