package main

import (
	"fmt"
	"os"

	"github.com/pulumi/pulumi-terraform-migrate/pkg/tfmig"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/spf13/cobra"
)

var setUrnCmd = &cobra.Command{
	Use:   "set-urn --migration migration.json --tf-addr [tf-resource-address] --urn [pulumi-resource-urn]",
	Short: "Updates the migration.json resource associations to tie the given Terraform resource to the given Pulumi resource",
	Long: `Sets or updates the migration.json resource associations to tie the given Terraform resource to the given Pulumi resource.

This operates across all stacks tracked by migration.json, substituting the stack name intelligently.

Example:

  pulumi-terraform-migrate set-urn \
     --migration migration.json \
     --tf-addr aws_instance.instance1 \
     --urn "urn:pulumi:stack::project::aws:ec2/instance:Instance::my-instance"
`,

	Run: func(cmd *cobra.Command, args []string) {
		migrationFile := cmd.Flag("migration").Value.String()
		urn := cmd.Flag("urn").Value.String()
		tfAddress := cmd.Flag("tf-addr").Value.String()
		force, _ := cmd.Flags().GetBool("force")

		if urn == "" {
			fmt.Fprintf(os.Stderr, "Error: --urn flag is required\n")
			os.Exit(1)
		}

		if tfAddress == "" {
			fmt.Fprintf(os.Stderr, "Error: --tf-addr flag is required\n")
			os.Exit(1)
		}

		if err := setResourceUrn(migrationFile, tfAddress, urn, force); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(setUrnCmd)
	setUrnCmd.Flags().String("migration", "migration.json", "Path to migration.json file")
	setUrnCmd.Flags().String("urn", "", "URN identifying the Pulumi resource")
	setUrnCmd.Flags().String("tf-addr", "", "Address identifying the Terraform resource")
	setUrnCmd.Flags().Bool("force", false, "Force the operation even if it introduces new integrity errors")
	setUrnCmd.MarkFlagRequired("urn")
	setUrnCmd.MarkFlagRequired("tf-addr")
}

func setResourceUrn(migrationFile, tfAddress, urn string, force bool) error {
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

	// Track how many resources were updated
	matchCount := 0

	parsedURN, err := resource.ParseURN(urn)
	if err != nil {
		return err
	}

	// Iterate through all stacks and update matching resources
	for i := range mf.Migration.Stacks {
		stack := &mf.Migration.Stacks[i]
		stackName := stack.PulumiStack

		// Create a specialized URN with the stack name replaced
		stackNameToken, err := tokens.ParseStackName(stackName)
		if err != nil {
			return fmt.Errorf("invalid stack name %q: %w", stackName, err)
		}
		parsedURNSpecialized := parsedURN.RenameStack(stackNameToken)

		// Find and update matching resource, or add if not found
		found := false
		for j := range stack.Resources {
			res := &stack.Resources[j]
			if res.TFAddr == tfAddress {
				res.URN = string(parsedURNSpecialized)
				// Clear migrate flag if it was set to skip
				if res.Migrate == tfmig.MigrateModeSkip {
					res.Migrate = tfmig.MigrateModeEmpty
				}
				matchCount++
				found = true
				break
			}
		}

		// If not found in this stack, add a new resource entry
		if !found {
			stack.Resources = append(stack.Resources, tfmig.Resource{
				TFAddr: tfAddress,
				URN:    string(parsedURNSpecialized),
			})
			matchCount++
		}
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

	fmt.Printf("Updated URN for %d resource(s) with address %q in %s\n", matchCount, tfAddress, migrationFile)
	if afterErrorCount > beforeErrorCount {
		fmt.Printf("Warning: introduced %d new integrity error(s) (--force was used)\n", afterErrorCount-beforeErrorCount)
	} else if afterErrorCount < beforeErrorCount {
		fmt.Printf("Fixed %d integrity error(s)\n", beforeErrorCount-afterErrorCount)
	}
	return nil
}
