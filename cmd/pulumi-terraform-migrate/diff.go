package main

import (
	"context"
	"fmt"
	"os"

	"github.com/pulumi/pulumi-terraform-migrate/pkg/tfmig"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Compute and display migration diff for all resources",
	Long: `Analyzes all Terraform resources in the migration and computes their status.

For each Terraform resource, this command determines if it is:
- Skipped (ignored for migration purposes)
- Accounted for (has a corresponding Pulumi resource with URN mapping)
  - In Pulumi program (resource exists in code)
  - In Pulumi state (resource exists in state file)
  - Preview status (no-op, update, replace, or refresh issues)

Example:
  pulumi-terraform-migrate diff --migration migration.json`,

	Run: func(cmd *cobra.Command, args []string) {
		migrationFile := cmd.Flag("migration").Value.String()
		if err := runDiff(migrationFile); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(diffCmd)
	diffCmd.Flags().String("migration", "migration.json", "Path to migration.json file")
}

// DiffSummary contains the overall diff results
type DiffSummary struct {
	// How many total Terraform resources there are.
	TotalResources int

	// How many resources are in [ResourceSkipped] status.
	SkippedResources int

	// How many resources are in [ResourceNotTracked] status.
	NotTrackedResources int

	// How many resources are in [ResourceTranslated] status.
	TranslatedResources map[tfmig.TranslatedStatus]int
}

// How many resources are in [TranslatedStatusMigrated].
func (ds DiffSummary) MigratedResources() int {
	return ds.TranslatedResources[tfmig.TranslatedStatusMigrated]
}

func runDiff(migrationFile string) error {
	ctx := context.Background()

	// Load migration file
	mf, err := tfmig.LoadMigration(migrationFile)
	if err != nil {
		return fmt.Errorf("failed to load migration file: %w", err)
	}

	// Create workspace
	ws, err := auto.NewLocalWorkspace(ctx, auto.WorkDir(mf.Migration.PulumiSources))
	if err != nil {
		return fmt.Errorf("failed to create Pulumi workspace: %w", err)
	}

	// Process each stack
	for _, stackConfig := range mf.Migration.Stacks {
		fmt.Printf("\n=== Stack: %s ===\n\n", stackConfig.PulumiStack)

		// Load Terraform state to get all TF resources
		tfState, err := tfmig.LoadTerraformState(stackConfig.TFState)
		if err != nil {
			return fmt.Errorf("failed to load Terraform state: %w", err)
		}

		// Run preview with refresh to get resource status
		fmt.Printf("Running pulumi preview --refresh for stack %s...\n", stackConfig.PulumiStack)

		// Compute diff
		statusMap, err := tfmig.ComputeDiff(ctx, stackConfig, ws, tfState)
		if err != nil {
			return fmt.Errorf("failed to compute diff: %w", err)
		}

		// Build summary
		summary := DiffSummary{
			TotalResources:      len(statusMap),
			TranslatedResources: make(map[tfmig.TranslatedStatus]int),
		}

		for _, status := range statusMap {
			switch s := status.(type) {
			case *tfmig.ResourceSkipped:
				summary.SkippedResources++
			case *tfmig.ResourceNotTracked:
				summary.NotTrackedResources++
			case *tfmig.ResourceTranslated:
				summary.TranslatedResources[s.TranslatedStatus]++
			}
		}

		// Display summary
		fmt.Printf("\n--- Summary ---\n")
		fmt.Printf("Total Terraform resources:     %d\n", summary.TotalResources)
		fmt.Printf("Fully migrated resources:      %d\n", summary.TranslatedResources[tfmig.TranslatedStatusMigrated])
		fmt.Printf("Skipped resources:             %d\n", summary.SkippedResources)
		fmt.Printf("Not tracked resources:         %d\n", summary.NotTrackedResources)
		fmt.Printf("Translated resources:          %d\n",
			summary.TranslatedResources[tfmig.TranslatedStatusMigrated]+
				summary.TranslatedResources[tfmig.TranslatedStatusNeedsUpdate]+
				summary.TranslatedResources[tfmig.TranslatedStatusNeedsReplace]+
				summary.TranslatedResources[tfmig.TranslatedStatusNoState])
		fmt.Printf("  - No Pulumi state:           %d\n", summary.TranslatedResources[tfmig.TranslatedStatusNoState])
		fmt.Printf("  - Needs update:              %d\n", summary.TranslatedResources[tfmig.TranslatedStatusNeedsUpdate])
		fmt.Printf("  - Needs replace:             %d\n", summary.TranslatedResources[tfmig.TranslatedStatusNeedsReplace])
		fmt.Printf("\n")

		// Display detailed status for problematic resources
		needsUpdate := summary.TranslatedResources[tfmig.TranslatedStatusNeedsUpdate]
		needsReplace := summary.TranslatedResources[tfmig.TranslatedStatusNeedsReplace]
		noState := summary.TranslatedResources[tfmig.TranslatedStatusNoState]

		if needsReplace > 0 || needsUpdate > 0 || noState > 0 {
			fmt.Printf("--- Issues Detected ---\n\n")

			if needsReplace > 0 {
				fmt.Printf("Resources that will be REPLACED:\n")
				for tfAddr, status := range statusMap {
					if rt, ok := status.(*tfmig.ResourceTranslated); ok && rt.TranslatedStatus == tfmig.TranslatedStatusNeedsReplace {
						fmt.Printf("  - %s (URN: %s)\n", tfAddr, rt.URN)
					}
				}
				fmt.Printf("\n")
			}

			if needsUpdate > 0 {
				fmt.Printf("Resources that will be UPDATED:\n")
				for tfAddr, status := range statusMap {
					if rt, ok := status.(*tfmig.ResourceTranslated); ok && rt.TranslatedStatus == tfmig.TranslatedStatusNeedsUpdate {
						fmt.Printf("  - %s (URN: %s)\n", tfAddr, rt.URN)
					}
				}
				fmt.Printf("\n")
			}

			if noState > 0 {
				fmt.Printf("Resources with NO STATE (missing from Pulumi):\n")
				for tfAddr, status := range statusMap {
					if rt, ok := status.(*tfmig.ResourceTranslated); ok && rt.TranslatedStatus == tfmig.TranslatedStatusNoState {
						fmt.Printf("  - %s (URN: %s)\n", tfAddr, rt.URN)
					}
				}
				fmt.Printf("\n")
			}
		}

		// Success indicator
		if needsReplace == 0 && needsUpdate == 0 && noState == 0 && summary.NotTrackedResources == 0 {
			fmt.Printf("✓ All resources migrated successfully!\n\n")
		}
	}

	return nil
}
