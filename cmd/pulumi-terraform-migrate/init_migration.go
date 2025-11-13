package main

import (
	"fmt"
	"os"

	"github.com/pulumi/pulumi-terraform-migrate/pkg/tfmig"
	"github.com/spf13/cobra"
)

var initMigrationCmd = &cobra.Command{
	Use:   "init-migration",
	Short: "Initialize a migration.json file from Terraform workspace(s)",
	Long: `Drafts a migration.json file with recommended default mappings.

Discovers Terraform workspaces, extracts states for each, and writes migration.json file.

Example:
  pulumi-terraform-migrate init-migration --migration migration.json --tf-sources ./terraform-manifests --pulumi-sources ./pulumi-project`,

	Run: func(cmd *cobra.Command, args []string) {
		opts := tfmig.InitMigrationOptions{
			MigrationFile: cmd.Flag("migration").Value.String(),
			TFSources:     cmd.Flag("tf-sources").Value.String(),
			PulumiSources: cmd.Flag("pulumi-sources").Value.String(),
		}

		if err := tfmig.InitMigration(opts); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Successfully initialized migration file: %s\n", opts.MigrationFile)
	},
}

func init() {
	rootCmd.AddCommand(initMigrationCmd)
	initMigrationCmd.Flags().String("migration", "migration.json", "Path to migration.json file to create")
	initMigrationCmd.Flags().String("tf-sources", "", "Path to Terraform sources directory (required)")
	initMigrationCmd.Flags().String("pulumi-sources", "", "Path to Pulumi sources directory (required)")
	initMigrationCmd.MarkFlagRequired("tf-sources")
	initMigrationCmd.MarkFlagRequired("pulumi-sources")
}
