package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	translateMigrationFile string
	translateStackName     string
	tfAddr                 string
)

var translateStateCmd = &cobra.Command{
	Use:   "translate-state",
	Short: "Translate Terraform resource state to Pulumi resource state",
	Long: `Attempts to perform direct automated translation of a Terraform resource state to the corresponding Pulumi resource state.
This is helpful for resources that cannot be imported through the standard import mechanism.

Example:
  pulumi-terraform-migrate translate-state --migration migration.json --stack dev --tf-addr "module.acm.aws_acm_certificate.this[0]"`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("TODO: Translate state\n")
		fmt.Printf("  Migration file: %s\n", translateMigrationFile)
		fmt.Printf("  Stack: %s\n", translateStackName)
		fmt.Printf("  Terraform address: %s\n", tfAddr)
	},
}

func init() {
	rootCmd.AddCommand(translateStateCmd)
	translateStateCmd.Flags().StringVar(&translateMigrationFile, "migration", "", "Path to migration.json file (required)")
	translateStateCmd.Flags().StringVar(&translateStackName, "stack", "", "Pulumi stack name (required)")
	translateStateCmd.Flags().StringVar(&tfAddr, "tf-addr", "", "Terraform resource address (required)")
	translateStateCmd.MarkFlagRequired("migration")
	translateStateCmd.MarkFlagRequired("stack")
	translateStateCmd.MarkFlagRequired("tf-addr")
}
