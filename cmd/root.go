package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "pulumi-terraform-migrate",
	Short: "A tool for migrating Terraform to Pulumi",
	Long: `pulumi-terraform-migrate is a tool for migrating Terraform state to Pulumi.
It reads a terraform state file and, given an initialized Pulumi program, it produces a Pulumi state file for importing into the program.

The state can be imported via "pulumi stack import --file <pulumi-state-file>".
`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
