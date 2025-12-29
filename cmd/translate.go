// Copyright 2016-2025, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"fmt"

	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg"
	"github.com/spf13/cobra"
)

func newTranslateCmd() *cobra.Command {
	var inputPath string
	var outputFile string
	var stackFolder string
	var requiredProvidersOutputFile string

	cmd := &cobra.Command{
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

	cmd.Flags().StringVarP(&inputPath, "input-path", "i", "", "Path to the Terraform state file")
	cmd.Flags().StringVarP(&outputFile, "output-file", "o", "", "Path to the output Pulumi state file")
	cmd.Flags().StringVarP(&requiredProvidersOutputFile, "required-providers-file", "r", "", "Path to output the required providers of the generated Pulumi state file")
	cmd.Flags().StringVarP(&stackFolder, "stack-folder", "s", "", "Path to the Pulumi stack folder")

	cmd.MarkFlagRequired("input-path")
	cmd.MarkFlagRequired("stack-folder")
	cmd.MarkFlagRequired("output-file")

	return cmd
}

func init() {
	rootCmd.AddCommand(newTranslateCmd())
}
