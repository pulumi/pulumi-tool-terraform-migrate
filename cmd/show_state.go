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
	"encoding/json"
	"fmt"

	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tofu"
	"github.com/spf13/cobra"
)

var (
	stateFilePath string
	projectDir    string
	workspace     string
)

var showStateCmd = &cobra.Command{
	Use:   "show-state",
	Short: "Load and display Terraform/OpenTOFU state as JSON",
	Long: `Load a Terraform or OpenTOFU state file and display it as pretty-printed JSON.

This command uses 'tofu show -json' internally to convert the state to the standard format.
It can load state from either a state file or a workspace.

Note: tofu be available in PATH.

This command may attempt running 'tofu init -upgrade' on a temporary workspace.

Examples:

  # Load from a state file
  pulumi-terraform-migrate show-state --state-file terraform.tfstate

  # Load from a project directory (default workspace)
  pulumi-terraform-migrate show-state --project-dir /path/to/terraform/project

  # Load from a specific workspace
  pulumi-terraform-migrate show-state --project-dir /path/to/terraform/project --workspace dev
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if stateFilePath == "" && projectDir == "" {
			return fmt.Errorf("either --state-file or --project-dir must be specified")
		}

		if stateFilePath != "" && workspace != "" {
			return fmt.Errorf("--workspace is not compatible with --state-file")
		}

		opts := tofu.LoadTerraformStateOptions{
			StateFilePath: stateFilePath,
			ProjectDir:    projectDir,
			Workspace:     workspace,
		}

		state, err := tofu.LoadTerraformState(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("failed to load Terraform state: %w", err)
		}

		// Pretty-print the state as JSON
		jsonBytes, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal state to JSON: %w", err)
		}

		fmt.Println(string(jsonBytes))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(showStateCmd)

	showStateCmd.Flags().StringVar(&stateFilePath, "state-file", "", "Path to the explicit terraform.tfstate file")
	showStateCmd.Flags().StringVar(&projectDir, "project-dir", "", "Path to the root directory where Terraform sources are located")
	showStateCmd.Flags().StringVar(&workspace, "workspace", "", "Terraform/OpenTOFU workspace to load (requires --project-dir)")
}
