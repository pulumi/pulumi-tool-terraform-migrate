package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pulumi/pulumi-terraform-migrate/pkg/tfmig"
	"github.com/spf13/cobra"
)

var resolveImportStubsCmd = &cobra.Command{
	Use:   "resolve-import-stubs",
	Short: "Resolve import stubs by mapping Terraform state to Pulumi resources",
	Long: `Attempts to resolve import IDs where possible to prepare for the import operation.
Not all resources can be imported.

Example:
  pulumi-terraform-migrate resolve-import-stubs --migration migration.json --stack dev --stubs import-stub.json --out import.json`,
	Run: func(cmd *cobra.Command, args []string) {
		opts := tfmig.ResolveImportStubsOptions{
			MigrationFile: cmd.Flag("migration").Value.String(),
			StackName:     cmd.Flag("stack").Value.String(),
			StubsFile:     cmd.Flag("stubs").Value.String(),
		}
		outputFile := cmd.Flag("out").Value.String()

		result, err := tfmig.ResolveImportStubs(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		outputData, err := json.MarshalIndent(result.ImportFile, "", "  ")
		if err != nil {
			err = fmt.Errorf("failed to marshal output: %w", err)
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(outputFile, outputData, 0644); err != nil {
			err = fmt.Errorf("failed to write output file: %w", err)
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if result.UnresolvedCount > 0 {
			fmt.Fprintf(os.Stderr, "\nWarning: for %d resources the IDs could not be resolved.\n", result.UnresolvedCount)
		}
	},
}

func init() {
	rootCmd.AddCommand(resolveImportStubsCmd)
	resolveImportStubsCmd.Flags().String("migration", "", "Path to migration.json file (required)")
	resolveImportStubsCmd.Flags().String("stack", "", "Pulumi stack name (required)")
	resolveImportStubsCmd.Flags().String("stubs", "", "Path to import-stub.json file (required)")
	resolveImportStubsCmd.Flags().String("out", "", "Path to output import.json file (required)")
	resolveImportStubsCmd.MarkFlagRequired("migration")
	resolveImportStubsCmd.MarkFlagRequired("stack")
	resolveImportStubsCmd.MarkFlagRequired("stubs")
	resolveImportStubsCmd.MarkFlagRequired("out")
}
