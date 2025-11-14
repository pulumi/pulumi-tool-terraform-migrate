package main

import (
	"fmt"
	"os"

	"github.com/pulumi/pulumi-terraform-migrate/pkg/tfmig"
	"github.com/spf13/cobra"
)

var untrackCmd = &cobra.Command{
	Use:   "untrack [tf-resource-address]",
	Short: "Remove a Terraform resource from migration tracking",
	Long: `Removes a specific Terraform resource from migration.json by deleting its entry
from all stacks in the migration file.

Example:
  pulumi-terraform-migrate untrack --migration migration.json aws_instance.example`,

	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		migrationFile := cmd.Flag("migration").Value.String()
		tfAddress := args[0]
		force, _ := cmd.Flags().GetBool("force")

		if err := untrackResource(migrationFile, tfAddress, force); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(untrackCmd)
	untrackCmd.Flags().String("migration", "migration.json", "Path to migration.json file")
	untrackCmd.Flags().Bool("force", false, "Force the operation even if it introduces new integrity errors")
}

func untrackResource(migrationFile, tfAddress string, force bool) error {
	// Load the migration file
	mf, err := tfmig.LoadMigration(migrationFile)
	if err != nil {
		return fmt.Errorf("failed to load migration file: %w", err)
	}

	// Run integrity checks before the edit
	beforeResult, err := tfmig.CheckMigrationIntegrity(mf)
	if err != nil {
		return fmt.Errorf("failed to check migration integrity before edit: %w", err)
	}
	beforeErrorCount := len(beforeResult.Errors)

	// Track how many resources were removed
	removeCount := 0

	// Iterate through all stacks and remove matching resources
	for i := range mf.Migration.Stacks {
		stack := &mf.Migration.Stacks[i]
		var filteredResources []tfmig.Resource

		for j := range stack.Resources {
			res := &stack.Resources[j]
			if res.TFAddr == tfAddress {
				// Skip this resource (don't add it to filtered list)
				removeCount++
			} else {
				// Keep this resource
				filteredResources = append(filteredResources, *res)
			}
		}

		// Replace resources with filtered list
		stack.Resources = filteredResources
	}

	if removeCount == 0 {
		return fmt.Errorf("no resources found with address %q", tfAddress)
	}

	// Run integrity checks after the edit
	afterResult, err := tfmig.CheckMigrationIntegrity(mf)
	if err != nil {
		return fmt.Errorf("failed to check migration integrity after edit: %w", err)
	}
	afterErrorCount := len(afterResult.Errors)

	// Check if the edit introduced new errors
	if afterErrorCount > beforeErrorCount && !force {
		return fmt.Errorf("operation would introduce %d new integrity error(s) (had %d, now would have %d). Use --force to proceed anyway",
			afterErrorCount-beforeErrorCount, beforeErrorCount, afterErrorCount)
	}

	// Save the modified migration file
	if err := mf.Save(migrationFile); err != nil {
		return fmt.Errorf("failed to save migration file: %w", err)
	}

	fmt.Printf("Removed %d resource(s) with address %q from %s\n", removeCount, tfAddress, migrationFile)
	if afterErrorCount > beforeErrorCount {
		fmt.Printf("Warning: introduced %d new integrity error(s) (--force was used)\n", afterErrorCount-beforeErrorCount)
	} else if afterErrorCount < beforeErrorCount {
		fmt.Printf("Fixed %d integrity error(s)\n", beforeErrorCount-afterErrorCount)
	}
	return nil
}
