package main

import (
	"context"
	"fmt"
	"os"
	"sort"

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
		details, _ := cmd.Flags().GetBool("details")
		if err := runDiff(migrationFile, details); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(diffCmd)
	diffCmd.Flags().String("migration", "migration.json", "Path to migration.json file")
	diffCmd.Flags().Bool("details", false, "Show detailed list of resources with issues")
}

// DiffSummary contains the overall diff results
type DiffSummary struct {
	// How many total Terraform resources there are.
	TotalResources int

	// How many resources are in [ResourceSkipped] status.
	SkippedResources int

	// How many resources are in [ResourceNotTracked] status.
	NotTrackedResources int

	// How many resources are in [ResourceNotTranslated] status.
	NotTranslatedResources int

	// How many resources are in [ResourceTranslated] status.
	TranslatedResources map[tfmig.TranslatedStatus]int
}

// How many resources are in [TranslatedStatusMigrated].
func (ds DiffSummary) MigratedResources() int {
	return ds.TranslatedResources[tfmig.TranslatedStatusMigrated]
}

func runDiff(migrationFile string, details bool) error {
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
		fmt.Printf("Terraform project: %s\n", mf.Migration.TFSources)
		fmt.Printf("Terraform state:   %s\n", stackConfig.TFState)
		fmt.Printf("Pulumi project:    %s\n", mf.Migration.PulumiSources)
		fmt.Printf("\n")

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
			case *tfmig.ResourceNotTranslated:
				summary.NotTranslatedResources++
			case *tfmig.ResourceTranslated:
				summary.TranslatedResources[s.TranslatedStatus]++
			}
		}

		// Display summary
		fmt.Printf("\n--- Summary ---\n\n")
		fmt.Printf("Total Terraform resources:     %d\n", summary.TotalResources)
		fmt.Printf("Fully migrated resources:      %d\n", summary.TranslatedResources[tfmig.TranslatedStatusMigrated])
		fmt.Printf("Skipped resources:             %d\n", summary.SkippedResources)
		fmt.Printf("Not tracked resources:         %d\n", summary.NotTrackedResources)
		fmt.Printf("Not translated resources:      %d\n", summary.NotTranslatedResources)
		fmt.Printf("Translated resources:          %d\n",
			summary.TranslatedResources[tfmig.TranslatedStatusMigrated]+
				summary.TranslatedResources[tfmig.TranslatedStatusNeedsUpdate]+
				summary.TranslatedResources[tfmig.TranslatedStatusNeedsReplace])
		fmt.Printf("  - Needs update:              %d\n", summary.TranslatedResources[tfmig.TranslatedStatusNeedsUpdate])
		fmt.Printf("  - Needs replace:             %d\n", summary.TranslatedResources[tfmig.TranslatedStatusNeedsReplace])
		fmt.Printf("\n")

		// Display detailed status for problematic resources if --details flag is set
		if details {
			notTracked := summary.NotTrackedResources
			notTranslated := summary.NotTranslatedResources
			needsUpdate := summary.TranslatedResources[tfmig.TranslatedStatusNeedsUpdate]
			needsReplace := summary.TranslatedResources[tfmig.TranslatedStatusNeedsReplace]

			if notTracked > 0 || notTranslated > 0 || needsUpdate > 0 || needsReplace > 0 {
				fmt.Printf("--- Issues Detected ---\n\n")

				// 1. Not tracked resources (easiest to fix)
				if notTracked > 0 {
					fmt.Printf("NOT TRACKED resources (%d):\n", notTracked)
					fmt.Printf("These resources need to be added to migration.json.\n\n")

					var addrs []string
					for tfAddr, status := range statusMap {
						if _, ok := status.(*tfmig.ResourceNotTracked); ok {
							addrs = append(addrs, tfAddr)
						}
					}
					sort.Strings(addrs)

					for _, tfAddr := range addrs {
						fmt.Printf("  - %s\n", tfAddr)
					}
					fmt.Printf("\n")
				}

				// 2. Not translated resources
				if notTranslated > 0 {
					fmt.Printf("NOT TRANSLATED resources (%d):\n", notTranslated)
					fmt.Printf("These resources are tracked but not found in Pulumi preview.\n")
					fmt.Printf("They need to be translated to Pulumi source code.\n\n")

					var addrs []string
					for tfAddr, status := range statusMap {
						if _, ok := status.(*tfmig.ResourceNotTranslated); ok {
							addrs = append(addrs, tfAddr)
						}
					}
					sort.Strings(addrs)

					for _, tfAddr := range addrs {
						fmt.Printf("  - %s\n", tfAddr)
					}
					fmt.Printf("\n")
				}

				// 3. Needs update resources
				if needsUpdate > 0 {
					fmt.Printf("NEEDS UPDATE resources (%d):\n", needsUpdate)
					fmt.Printf("These resources will be updated when you run pulumi up.\n\n")

					var addrs []string
					for tfAddr, status := range statusMap {
						if rt, ok := status.(*tfmig.ResourceTranslated); ok && rt.TranslatedStatus == tfmig.TranslatedStatusNeedsUpdate {
							addrs = append(addrs, tfAddr)
						}
					}
					sort.Strings(addrs)

					for _, tfAddr := range addrs {
						status := statusMap[tfAddr].(*tfmig.ResourceTranslated)
						fmt.Printf("  - %s\n    URN: %s\n", tfAddr, status.URN)
					}
					fmt.Printf("\n")
				}

				// 4. Needs replace resources
				if needsReplace > 0 {
					fmt.Printf("NEEDS REPLACE resources (%d):\n", needsReplace)
					fmt.Printf("WARNING: These resources will be REPLACED (destroyed and recreated) when you run pulumi up.\n\n")

					var addrs []string
					for tfAddr, status := range statusMap {
						if rt, ok := status.(*tfmig.ResourceTranslated); ok && rt.TranslatedStatus == tfmig.TranslatedStatusNeedsReplace {
							addrs = append(addrs, tfAddr)
						}
					}
					sort.Strings(addrs)

					for _, tfAddr := range addrs {
						status := statusMap[tfAddr].(*tfmig.ResourceTranslated)
						fmt.Printf("  - %s\n    URN: %s\n", tfAddr, status.URN)
					}
					fmt.Printf("\n")
				}
			}
		}

		// Success indicator
		needsUpdate := summary.TranslatedResources[tfmig.TranslatedStatusNeedsUpdate]
		needsReplace := summary.TranslatedResources[tfmig.TranslatedStatusNeedsReplace]

		if needsReplace == 0 && needsUpdate == 0 && summary.NotTrackedResources == 0 && summary.NotTranslatedResources == 0 {
			fmt.Printf("✓ All resources migrated successfully!\n\n")
		}
	}

	return nil
}
