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
	Long: `Marks a specific Terraform resource to be skipped during migration by setting the appropriate migrate mode
for that resource in all stacks in the migration.json file.

By default, sets migrate: "skip". Use flags to set refined skip states:
  --ignore-no-state:       Sets migrate: "ignore-no-state" (resource that did not finish migrating state)
  --ignore-needs-update:   Sets migrate: "ignore-needs-update" (resource has state but wants to update on preview)
  --ignore-needs-replace:  Sets migrate: "ignore-needs-replace" (resource has state but wants to replace on preview)

Examples:
  pulumi-terraform-migrate skip --migration migration.json aws_instance.example
  pulumi-terraform-migrate skip --ignore-no-state aws_instance.example
  pulumi-terraform-migrate skip --ignore-needs-update aws_instance.example
  pulumi-terraform-migrate skip --ignore-needs-replace aws_instance.example`,

	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		migrationFile := cmd.Flag("migration").Value.String()
		tfAddress := args[0]
		force, _ := cmd.Flags().GetBool("force")
		ignoreNoState, _ := cmd.Flags().GetBool("ignore-no-state")
		ignoreNeedsUpdate, _ := cmd.Flags().GetBool("ignore-needs-update")
		ignoreNeedsReplace, _ := cmd.Flags().GetBool("ignore-needs-replace")

		// Determine the migrate mode
		migrateMode := tfmig.MigrateModeSkip
		flagCount := 0
		if ignoreNoState {
			migrateMode = tfmig.MigrateModeIgnoreNoState
			flagCount++
		}
		if ignoreNeedsUpdate {
			migrateMode = tfmig.MigrateModeIgnoreNeedsUpdate
			flagCount++
		}
		if ignoreNeedsReplace {
			migrateMode = tfmig.MigrateModeIgnoreNeedsReplace
			flagCount++
		}

		// Check that only one flag is set
		if flagCount > 1 {
			fmt.Fprintf(os.Stderr, "Error: only one of --ignore-no-state, --ignore-needs-update, or --ignore-needs-replace can be specified\n")
			os.Exit(1)
		}

		if err := skipResource(migrationFile, tfAddress, migrateMode, force); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(skipCmd)
	skipCmd.Flags().String("migration", "migration.json", "Path to migration.json file")
	skipCmd.Flags().Bool("force", false, "Force the operation even if it introduces new integrity errors")
	skipCmd.Flags().Bool("ignore-no-state", false, "Set migrate mode to 'ignore-no-state'")
	skipCmd.Flags().Bool("ignore-needs-update", false, "Set migrate mode to 'ignore-needs-update'")
	skipCmd.Flags().Bool("ignore-needs-replace", false, "Set migrate mode to 'ignore-needs-replace'")
}

func skipResource(migrationFile, tfAddress string, migrateMode tfmig.MigrateMode, force bool) error {
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

	// Track how many resources were marked with the specified migrate mode
	matchCount := 0

	// Iterate through all stacks and mark matching resources with the specified migrate mode
	for i := range mf.Migration.Stacks {
		stack := &mf.Migration.Stacks[i]
		for j := range stack.Resources {
			res := &stack.Resources[j]
			if res.TFAddr == tfAddress {
				res.Migrate = migrateMode
				matchCount++
			}
		}
	}

	if matchCount == 0 {
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

	fmt.Printf("Marked %d resource(s) with address %q as %q in %s\n", matchCount, tfAddress, migrateMode, migrationFile)
	if afterErrorCount > beforeErrorCount {
		fmt.Printf("Warning: introduced %d new integrity error(s) (--force was used)\n", afterErrorCount-beforeErrorCount)
	} else if afterErrorCount < beforeErrorCount {
		fmt.Printf("Fixed %d integrity error(s)\n", beforeErrorCount-afterErrorCount)
	}
	return nil
}
