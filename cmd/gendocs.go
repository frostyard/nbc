/*
Copyright © 2025 Brian Ketelsen <bketelsen@gmail.com>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/
package cmd

import (
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

// NewGendocsCommand creates a new command to generate documentation for the project
func NewGendocsCommand() *cobra.Command {
	// gendocsCmd represents the gendocs command
	gendocsCmd := &cobra.Command{
		Use:    "gendocs",
		Hidden: true,
		Short:  "Generates documentation for the project",
		Long: `Generates documentation for the command using the cobra doc generator.
The documentation is generated in the ./content/docs/cli directory and
is in markdown format.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			lipgloss.DefaultRenderer().SetColorProfile(termenv.Ascii)

			o, err := cmd.Flags().GetString("output")
			if err != nil {
				return err
			}
			cmd.Root().DisableAutoGenTag = true
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			target := filepath.Join(wd, o)
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			return doc.GenMarkdownTreeCustom(cmd.Root(), o, func(_ string) string {
				return ""
			}, func(s string) string {
				return s
			})
		},
	}

	// Define cobra flags, the default value has the lowest (least significant) precedence
	gendocsCmd.Flags().StringP("output", "o", "docs/cli", "Output directory for the documentation (default is docs)")
	return gendocsCmd
}

func init() {
	rootCmd.AddCommand(NewGendocsCommand())
}
