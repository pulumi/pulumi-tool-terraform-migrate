package main

import (
	"fmt"
	"os"

	"github.com/pulumi/pulumi-terraform-migrate/pkg/tfmig"
	"github.com/spf13/cobra"
)

var skipCmd = &cobra.Command{
	Use:   "skip [tf-resource-address]",
	Short: "Mark a Terraform resource to be skipped during migration",
	Long: `Marks a specific Terraform resource to be skipped during migration by setting migrate: "skip"
for that resource in all stacks in the migration.json file.

Example:
  pulumi-terraform-migrate skip --migration migration.json aws_instance.example`,

	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		migrationFile := cmd.Flag("migration").Value.String()
		tfAddress := args[0]

		if err := skipResource(migrationFile, tfAddress); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(skipCmd)
	skipCmd.Flags().String("migration", "migration.json", "Path to migration.json file")
}

func skipResource(migrationFile, tfAddress string) error {
	// Load the migration file
	mf, err := tfmig.LoadMigration(migrationFile)
	if err != nil {
		return fmt.Errorf("failed to load migration file: %w", err)
	}

	// Track how many resources were marked as skip
	matchCount := 0

	// Iterate through all stacks and mark matching resources as skip
	for i := range mf.Migration.Stacks {
		stack := &mf.Migration.Stacks[i]
		for j := range stack.Resources {
			res := &stack.Resources[j]
			if res.TFAddr == tfAddress {
				res.Migrate = "skip"
				matchCount++
			}
		}
	}

	if matchCount == 0 {
		return fmt.Errorf("no resources found with address %q", tfAddress)
	}

	// Save the modified migration file
	if err := mf.Save(migrationFile); err != nil {
		return fmt.Errorf("failed to save migration file: %w", err)
	}

	fmt.Printf("Marked %d resource(s) with address %q as skip in %s\n", matchCount, tfAddress, migrationFile)
	return nil
}
