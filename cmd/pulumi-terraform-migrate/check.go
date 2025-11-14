package main

import (
	"fmt"
	"os"

	"github.com/pulumi/pulumi-terraform-migrate/pkg/tfmig"
	"github.com/spf13/cobra"
)

// checkCmd represents the check command
var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check migration.json for integrity issues",
	Long: `Performs integrity checks on migration.json:

1. Verifies that all referenced files exist
2. Checks for unique bidirectional mapping between tf-addr and URN
3. Validates that resources in migration.json match the Terraform state`,
	Args: cobra.NoArgs,
	RunE: runCheck,
}

var checkMigrationFile string

func init() {
	rootCmd.AddCommand(checkCmd)
	checkCmd.Flags().StringVar(&checkMigrationFile, "migration", "migration.json", "Path to migration.json file")
}

func runCheck(cmd *cobra.Command, args []string) error {
	// Load the migration file
	migrationFile, err := tfmig.LoadMigration(checkMigrationFile)
	if err != nil {
		return fmt.Errorf("failed to load migration file: %w", err)
	}

	// Perform integrity checks
	result, err := tfmig.CheckMigrationIntegrity(migrationFile)
	if err != nil {
		return fmt.Errorf("failed to check migration integrity: %w", err)
	}

	// Display results
	if !result.HasErrors() {
		fmt.Println("✓ All integrity checks passed")
		return nil
	}

	// Group errors by category
	errorsByCategory := make(map[string][]string)
	for _, err := range result.Errors {
		errorsByCategory[err.Category] = append(errorsByCategory[err.Category], err.Message)
	}

	fmt.Printf("✗ Found %d integrity issue(s):\n\n", len(result.Errors))

	// Display errors grouped by category
	categories := []string{"file-existence", "unique-mapping", "state-consistency"}
	categoryTitles := map[string]string{
		"file-existence":    "File Existence Errors",
		"unique-mapping":    "Unique Mapping Errors",
		"state-consistency": "State Consistency Errors",
	}

	for _, category := range categories {
		if _, ok := errorsByCategory[category]; ok {
			fmt.Printf("## %s\n", categoryTitles[category])

			// Collect unique suggestions for this category
			suggestionSeen := false
			var exampleSuggestion string

			for _, checkErr := range result.Errors {
				if checkErr.Category == category {
					fmt.Printf("  • %s\n", checkErr.Message)
					if checkErr.Suggestion != "" && !suggestionSeen {
						exampleSuggestion = checkErr.Suggestion
						suggestionSeen = true
					}
				}
			}

			// Display suggestion pattern once per category
			if exampleSuggestion != "" {
				fmt.Printf("\n  → Example resolution: %s\n", exampleSuggestion)
			}

			fmt.Println()
		}
	}

	os.Exit(1)
	return nil
}
