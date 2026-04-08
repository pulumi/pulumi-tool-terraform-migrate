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

func newModuleMapCmd() *cobra.Command {
	var from string
	var stateFile string
	var out string
	var pulumiStack string
	var pulumiProject string

	cmd := &cobra.Command{
		Use:   "module-map",
		Short: "Generate a module-map.json sidecar from Terraform sources and state",
		Long: `Generate a module-map.json sidecar file that describes Terraform module
instances, their interfaces (inputs/outputs), and the Pulumi URNs of
resources belonging to each module instance.

Example:

  pulumi-terraform-migrate module-map \
    --from path/to/terraform-sources \
    --state-file path/to/terraform.tfstate \
    --out /tmp/module-map.json \
    --pulumi-stack dev \
    --pulumi-project myproject

The --state-file flag accepts either a raw .tfstate file or the JSON output
of 'tofu show -json'. The format is auto-detected.

When a raw .tfstate is provided the tool also evaluates variable expressions
using the OpenTofu evaluation engine, populating evaluatedValue fields in
the output. If evaluation fails, the tool continues gracefully without
evaluated values.
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := pkg.GenerateModuleMap(cmd.Context(), from, stateFile, out, pulumiStack, pulumiProject)
			if err != nil {
				return fmt.Errorf("failed to generate module map: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&from, "from", "f", "", "Path to the Terraform root folder")
	cmd.Flags().StringVar(&stateFile, "state-file", "", "Path to terraform.tfstate or tofu show -json output")
	cmd.Flags().StringVarP(&out, "out", "o", "", "Where to emit the module-map.json file")
	cmd.Flags().StringVar(&pulumiStack, "pulumi-stack", "", "Pulumi stack name for URN generation")
	cmd.Flags().StringVar(&pulumiProject, "pulumi-project", "", "Pulumi project name for URN generation")

	cmd.MarkFlagRequired("from")
	cmd.MarkFlagRequired("state-file")
	cmd.MarkFlagRequired("out")
	cmd.MarkFlagRequired("pulumi-stack")
	cmd.MarkFlagRequired("pulumi-project")

	return cmd
}

func init() {
	rootCmd.AddCommand(newModuleMapCmd())
}
