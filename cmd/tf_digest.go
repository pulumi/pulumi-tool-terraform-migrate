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

func newTfDigestCmd() *cobra.Command {
	var from string
	var stateFile string
	var out string
	var pulumiStack string
	var pulumiProject string
	var hostname string
	var organization string
	var workspace string
	var tokenEnv string
	var projectDir string
	var skipSecrets bool
	var runtime string

	cmd := &cobra.Command{
		Use:   "tf-digest",
		Short: "Digest Terraform sources and state into a tf-digest.json sidecar",
		Long: `Digest Terraform configuration and state into a tf-digest.json sidecar
file that describes Terraform module instances, their interfaces
(inputs/outputs), and the Pulumi URNs of resources belonging to each
module instance.

State can be provided as a local file (--state-file) or pulled from a
TFC-compatible remote backend (--hostname, --organization, --workspace,
--token-env).

Examples:

  # From a local state file
  pulumi-terraform-migrate tf-digest \
    --from path/to/terraform-sources \
    --state-file path/to/terraform.tfstate \
    --out /tmp/tf-digest.json \
    --pulumi-stack dev \
    --pulumi-project myproject

  # From a TFC-compatible remote (Scalr, TFC, TFE)
  pulumi-terraform-migrate tf-digest \
    --from path/to/terraform-sources \
    --hostname app.terraform.io \
    --organization my-org \
    --workspace my-workspace-dev \
    --token-env TFC_TOKEN \
    --out /tmp/tf-digest.json \
    --pulumi-stack dev \
    --pulumi-project myproject
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate mutually exclusive state sources.
			remoteFlags := []string{"hostname", "organization", "workspace", "token-env"}
			remoteValues := map[string]string{
				"hostname":     hostname,
				"organization": organization,
				"workspace":    workspace,
				"token-env":    tokenEnv,
			}
			hasAnyRemote := false
			for _, v := range remoteValues {
				if v != "" {
					hasAnyRemote = true
					break
				}
			}

			if stateFile != "" && hasAnyRemote {
				return fmt.Errorf("--state-file and remote flags (--hostname, --organization, --workspace, --token-env) are mutually exclusive")
			}

			if stateFile == "" && !hasAnyRemote {
				return fmt.Errorf("either --state-file or all remote flags (--hostname, --organization, --workspace, --token-env) must be provided")
			}

			var remote *pkg.RemoteStateOptions
			if hasAnyRemote {
				var missing []string
				for _, flag := range remoteFlags {
					if remoteValues[flag] == "" {
						missing = append(missing, "--"+flag)
					}
				}
				if len(missing) > 0 {
					return fmt.Errorf("--hostname, --organization, --workspace, and --token-env are all required when using remote state (missing: %s)",
						strings.Join(missing, ", "))
				}

				token := os.Getenv(tokenEnv)
				if token == "" {
					return fmt.Errorf("environment variable %s is empty or not set", tokenEnv)
				}

				remote = &pkg.RemoteStateOptions{
					Hostname:     hostname,
					Organization: organization,
					Workspace:    workspace,
					Token:        token,
				}
			}

			secretsOpts := &pkg.SecretsOptions{
				ProjectDir:  projectDir,
				ProjectName: pulumiProject,
				Runtime:     runtime,
				Skip:        skipSecrets,
			}

			err := pkg.GenerateModuleMap(cmd.Context(), from, stateFile, out, pulumiStack, pulumiProject, remote, secretsOpts)
			if err != nil {
				// Enrich authentication errors with the env var name for user guidance.
				if remote != nil && strings.Contains(err.Error(), "authentication failed") {
					return fmt.Errorf("%w: check token in env var %s", err, tokenEnv)
				}
				return fmt.Errorf("failed to generate tf digest: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&from, "from", "f", "", "Path to the Terraform root folder")
	cmd.Flags().StringVar(&stateFile, "state-file", "", "Path to terraform.tfstate or tofu show -json output")
	cmd.Flags().StringVarP(&out, "out", "o", "", "Where to emit the module-map.json file")
	cmd.Flags().StringVar(&pulumiStack, "pulumi-stack", "", "Pulumi stack name for URN generation")
	cmd.Flags().StringVar(&pulumiProject, "pulumi-project", "", "Pulumi project name for URN generation")
	cmd.Flags().StringVar(&hostname, "hostname", "", "TFC-compatible API hostname (e.g. app.terraform.io)")
	cmd.Flags().StringVar(&organization, "organization", "", "Organization name on the TFC-compatible host")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace name on the TFC-compatible host")
	cmd.Flags().StringVar(&tokenEnv, "token-env", "", "Name of environment variable containing the API token")
	cmd.Flags().StringVar(&projectDir, "project-dir", ".", "Path to the Pulumi project directory (for setting secrets)")
	cmd.Flags().BoolVar(&skipSecrets, "skip-secrets", false, "Skip setting sensitive attributes as Pulumi config secrets")
	cmd.Flags().StringVar(&runtime, "runtime", "", "Pulumi runtime to use when creating Pulumi.yaml (e.g. nodejs, python, go, yaml)")

	cmd.MarkFlagRequired("from")
	cmd.MarkFlagRequired("out")
	cmd.MarkFlagRequired("pulumi-stack")
	cmd.MarkFlagRequired("pulumi-project")

	return cmd
}

func init() {
	rootCmd.AddCommand(newTfDigestCmd())
}
