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

func newStackCmd() *cobra.Command {
	var from string
	var out string
	var to string
	var plugins string

	cmd := &cobra.Command{
		Use:   "stack",
		Short: "Translate Terraform state to a Pulumi stack state",
		Long: `Translate Terraform state to a Pulumi stack state.

Example:

  pulumi-terraform-migrate stack \
    --from path/to/terraform-sources \
    --to path/to/pulumi-project \
    --out /tmp/pulumi-state.json \
    --plugins /tmp/required-plugins.json

The translated state picks recommended Pulumi providers and resource types to represent every Terraform resource
present in the source.

Before running this tool, '--to path/to/pulumi-project' should contain a valid Pulumi project with a
currently selected stack that already has initial state ('pulumi stack export' succeeds).

Generated 'pulumi-state.json' file is in the format compatible with importing into a Pulumi project:

  pulumi stack import --file pulumi-state.json

Setting the optional '--plugins' parameter generates a 'required-plugins.json' such as '[{"name":"aws", "version":"7.12.0"}]'.
This file recommends Pulumi plugins and versions to install into the project, for example:

  pulumi plugin install resource aws 7.12.0

The tool may run 'tofu', 'tofu init', 'tofu refresh' to extract the Terraform state and these commands may require
authorizing read-only access to the cloud accounts. The tool never runs destructive commands such as 'tofu apply'.

See also:

- pulumi stack import
  https://www.pulumi.com/docs/iac/cli/commands/pulumi_stack_import/

- pulumi plugin install
  https://www.pulumi.com/docs/iac/cli/commands/pulumi_plugin_install/
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := pkg.TranslateAndWriteState(cmd.Context(), from, to, out, plugins)
			if err != nil {
				return fmt.Errorf("failed to convert and write Terraform state: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&from, "from", "f", "", "Path to the Terraform root folder")
	cmd.Flags().StringVarP(&to, "to", "t", "", "Path to the Pulumi project folder")
	cmd.Flags().StringVarP(&out, "out", "o", "", "Where to emit the translated Pulumi stack file")
	cmd.Flags().StringVarP(&plugins, "plugins", "p", "", "Where to emit plugin requirements")

	cmd.MarkFlagRequired("from")
	cmd.MarkFlagRequired("to")
	cmd.MarkFlagRequired("out")

	return cmd
}

func init() {
	rootCmd.AddCommand(newStackCmd())
}
