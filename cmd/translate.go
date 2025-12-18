/*
Copyright © 2025 Pulumi Corporation
*/
package cmd

import (
	"fmt"

	"github.com/pulumi/pulumi-terraform-migrate/pkg"
	"github.com/spf13/cobra"
)

var (
	inputPath                   string
	outputFile                  string
	stackFolder                 string
	requiredProvidersOutputFile string
)

var translateCmd = &cobra.Command{
	Use:   "translate",
	Short: "Translate Terraform state files to Pulumi state format",
	Long: `This tool helps translate infrastructure state from Terraform to Pulumi. It requires a Terraform state file and the path to a folder containing an initialized Pulumi program.

Example:
  pulumi-terraform-migrate translate --input-path terraform.tfstate --output-file pulumi.json --stack-folder path/to/pulumi/stack --required-providers-file required-providers.json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Converting Terraform state from: %s\n", inputPath)
		fmt.Printf("Output will be written to: %s\n", outputFile)
		err := pkg.TranslateAndWriteState(cmd.Context(), inputPath, stackFolder, outputFile, requiredProvidersOutputFile)
		if err != nil {
			return fmt.Errorf("failed to convert and write Terraform state: %w", err)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(translateCmd)

	translateCmd.Flags().StringVarP(&inputPath, "input-path", "i", "", "Path to the Terraform state file")
	translateCmd.Flags().StringVarP(&outputFile, "output-file", "o", "", "Path to the output Pulumi state file")
	translateCmd.Flags().StringVarP(&requiredProvidersOutputFile, "required-providers-file", "r", "", "Path to output the required providers of the generated Pulumi state file")
	translateCmd.Flags().StringVarP(&stackFolder, "stack-folder", "s", "", "Path to the Pulumi stack folder")

	translateCmd.MarkFlagRequired("input-path")
	translateCmd.MarkFlagRequired("stack-folder")
	translateCmd.MarkFlagRequired("output-file")
}
