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
	"os"
	"strings"

	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg"
	"github.com/spf13/cobra"
)

func newStackCmd() *cobra.Command {
	var from string
	var out string
	var to string
	var plugins string
	var strict bool
	var noModuleComponents bool
	var moduleTypeMaps []string
	var pulumiStack string
	var pulumiProject string
	var moduleSchemas []string

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
authorizing read-only access to the cloud accounts. The tool never runs mutating commands such as 'tofu apply'.

See also:

- pulumi stack import
  https://www.pulumi.com/docs/iac/cli/commands/pulumi_stack_import/

- pulumi plugin install
  https://www.pulumi.com/docs/iac/cli/commands/pulumi_plugin_install/
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			typeOverrides := map[string]string{}
			for _, mapping := range moduleTypeMaps {
				parts := strings.SplitN(mapping, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid --module-type-map format %q, expected module.name=type:token", mapping)
				}
				typeOverrides[parts[0]] = parts[1]
			}

			schemaOverrides := map[string]string{}
			for _, mapping := range moduleSchemas {
				parts := strings.SplitN(mapping, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid --module-schema format %q, expected module.name=./path/to/schema.json", mapping)
				}
				schemaOverrides[parts[0]] = parts[1]
			}
			_ = schemaOverrides // TODO: wire into pipeline in Phase 2 (PR 9)

			enableComponents := !noModuleComponents
			if noModuleComponents && len(moduleTypeMaps) > 0 {
				fmt.Fprintf(os.Stderr, "Warning: --module-type-map is ignored when --no-module-components is set\n")
				typeOverrides = nil
			}

			err := pkg.TranslateAndWriteState(cmd.Context(), from, to, out, plugins, strict, enableComponents, typeOverrides, pulumiStack, pulumiProject)
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
	cmd.Flags().BoolVarP(&strict, "strict", "s", false, "Fail if any resources fail to be translated")
	cmd.Flags().BoolVar(&noModuleComponents, "no-module-components", false,
		"Disable creation of component resources for Terraform modules (flat mode)")
	cmd.Flags().StringArrayVar(&moduleTypeMaps, "module-type-map", nil,
		"Override component type token for a module (repeatable, format: module.name=pkg:mod:Type)")
	cmd.Flags().StringVar(&pulumiStack, "pulumi-stack", "", "Override Pulumi stack name (skip auto-detection)")
	cmd.Flags().StringVar(&pulumiProject, "pulumi-project", "", "Override Pulumi project name (skip auto-detection)")
	cmd.Flags().StringArrayVar(&moduleSchemas, "module-schema", nil,
		"Pulumi package schema for component validation (repeatable, format: module.name=./path/to/schema.json)")

	cmd.MarkFlagRequired("from")
	cmd.MarkFlagRequired("to")
	cmd.MarkFlagRequired("out")

	return cmd
}

func init() {
	rootCmd.AddCommand(newStackCmd())
}
