/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pulumi/pulumi-terraform-migrate/pkg"
	"github.com/spf13/cobra"
)

var (
	inputPath   string
	outputFile  string
	stackFolder string
)

var translateCmd = &cobra.Command{
	Use:   "translate",
	Short: "Translate Terraform state files to Pulumi state format",
	Long: `This tool helps translate infrastructure state from Terraform to Pulumi. It requires a Terraform state file and the path to a folder containing an initialized Pulumi program.

Example:
  pulumi-terraform-state-translate --input-path terraform.tfstate --output-file pulumi.json --stack-folder stack`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Converting Terraform state from: %s\n", inputPath)
		fmt.Printf("Output will be written to: %s\n", outputFile)

		data, err := pkg.TranslateState(inputPath, stackFolder)
		if err != nil {
			return fmt.Errorf("failed to convert Terraform state: %w", err)
		}

		bytes, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("failed to marshal Pulumi state: %w", err)
		}
		err = os.WriteFile(outputFile, bytes, 0o600)
		if err != nil {
			return fmt.Errorf("failed to write Pulumi state: %w", err)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(translateCmd)

	translateCmd.Flags().StringVarP(&inputPath, "input-path", "i", "", "Path to the Terraform state file")
	translateCmd.Flags().StringVarP(&outputFile, "output-file", "o", "", "Path to the output Pulumi state file")
	translateCmd.Flags().StringVarP(&stackFolder, "stack-folder", "s", "", "Path to the Pulumi stack folder")

	translateCmd.MarkFlagRequired("input-path")
	translateCmd.MarkFlagRequired("stack-folder")
	translateCmd.MarkFlagRequired("output-file")
}
