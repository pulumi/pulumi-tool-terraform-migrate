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

func newSetSecretsCmd() *cobra.Command {
	var stateFile string
	var projectDir string
	var projectName string
	var stack string
	var runtime string
	var mappingStrs []string

	cmd := &cobra.Command{
		Use:   "set-secrets",
		Short: "Extract secret values from Terraform state and set them as Pulumi stack config secrets",
		Long: `Extract secret values from a Terraform state file and set them as encrypted
secrets in a Pulumi stack config. This allows the agent to orchestrate
secret migration without ever seeing the actual secret values.

The --map flag specifies a mapping from Pulumi config key to Terraform
resource address and attribute name:

  --map configKey=terraform.address:attribute

Example:

  pulumi-terraform-migrate set-secrets \
    --state-file terraform.tfstate \
    --project-dir ./pulumi \
    --stack prod \
    --map 'dbPassword=aws_ssm_parameter.db_password:value' \
    --map 'apiKey=aws_secretsmanager_secret_version.api_key:secret_string'

The stack is initialized automatically if it doesn't exist.
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var mappings []pkg.SecretMapping
			for _, s := range mappingStrs {
				m, err := pkg.ParseSecretMapping(s)
				if err != nil {
					return err
				}
				mappings = append(mappings, m)
			}

			if len(mappings) == 0 {
				return fmt.Errorf("at least one --map flag is required")
			}

			return pkg.SetSecrets(stateFile, projectDir, projectName, stack, runtime, mappings)
		},
	}

	cmd.Flags().StringVar(&stateFile, "state-file", "", "Path to terraform.tfstate")
	cmd.Flags().StringVar(&projectDir, "project-dir", ".", "Path to the Pulumi project directory")
	cmd.Flags().StringVar(&projectName, "project-name", "", "Pulumi project name (used when creating Pulumi.yaml)")
	cmd.Flags().StringVarP(&stack, "stack", "s", "", "Pulumi stack name")
	cmd.Flags().StringArrayVar(&mappingStrs, "map", nil, "Secret mapping: configKey=terraformAddress:attribute")
	cmd.Flags().StringVar(&runtime, "runtime", "", "Pulumi runtime to use when creating Pulumi.yaml (e.g. nodejs, python, go, yaml)")

	cmd.MarkFlagRequired("state-file")
	cmd.MarkFlagRequired("stack")

	return cmd
}

func init() {
	rootCmd.AddCommand(newSetSecretsCmd())
}
