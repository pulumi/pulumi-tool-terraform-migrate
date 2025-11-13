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

		if urn == "" {
			fmt.Fprintf(os.Stderr, "Error: --urn flag is required\n")
			os.Exit(1)
		}

		if tfAddress == "" {
			fmt.Fprintf(os.Stderr, "Error: --tf-addr flag is required\n")
			os.Exit(1)
		}

		if err := setResourceUrn(migrationFile, tfAddress, urn); err != nil {
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
	setUrnCmd.MarkFlagRequired("urn")
	setUrnCmd.MarkFlagRequired("tf-addr")
}

func setResourceUrn(migrationFile, tfAddress, urn string) error {
	// Load the migration file
	mf, err := tfmig.LoadMigration(migrationFile)
	if err != nil {
		return fmt.Errorf("failed to load migration file: %w", err)
	}

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
				if res.Migrate == "skip" {
					res.Migrate = ""
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

	// Save the modified migration file
	if err := mf.Save(migrationFile); err != nil {
		return fmt.Errorf("failed to save migration file: %w", err)
	}

	fmt.Printf("Updated URN for %d resource(s) with address %q in %s\n", matchCount, tfAddress, migrationFile)
	return nil
}
