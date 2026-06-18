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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newPatchStateCmd() *cobra.Command {
	var statePath string
	var digestPath string
	var fieldsPath string
	var mappingFile string
	var outPath string
	var projectDir string
	var stack string
	var configDir string

	cmd := &cobra.Command{
		Use:   "patch-state",
		Short: "Patch imported state with not_read field values from TF digest",
		Long: `Patch a Pulumi stack state (from pulumi stack export) with field values
from a TF digest that the cloud API import doesn't return.

Only patches fields classified as "not_read" in the fields file. For each
matching resource, if the state input is nil:
  1. Use the digest value if available (from TF state)
  2. Fall back to the TF SDK default from the fields file

After patching, re-import the state with: pulumi stack import --file <output>

Example:

  pulumi stack export > state.json
  pulumi-terraform-migrate patch-state \
    --state state.json \
    --digest tf-digest.json \
    --fields aws-import-diff-fields.json \
    --mapping-file mappings.yaml \
    --out patched-state.json
  pulumi stack import --file patched-state.json
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load state.
			stateData, err := os.ReadFile(statePath)
			if err != nil {
				return fmt.Errorf("reading state file: %w", err)
			}

			// Load digest.
			digestData, err := os.ReadFile(digestPath)
			if err != nil {
				return fmt.Errorf("reading digest: %w", err)
			}
			var digest pkg.ModuleMap
			if err := json.Unmarshal(digestData, &digest); err != nil {
				return fmt.Errorf("parsing digest: %w", err)
			}

			// Load fields.
			fieldsFile, err := pkg.LoadFieldsFile(fieldsPath)
			if err != nil {
				return err
			}

			// Load mappings.
			moduleMappings := make(map[string]string)
			resourceMappings := make(map[string]string)
			if mappingFile != "" {
				mfData, err := os.ReadFile(mappingFile)
				if err != nil {
					return fmt.Errorf("reading mapping file: %w", err)
				}
				var mf struct {
					Modules   map[string]string `yaml:"modules"`
					Mappings  map[string]string `yaml:"mappings"`
					Resources map[string]string `yaml:"resources"`
				}
				if err := yaml.Unmarshal(mfData, &mf); err != nil {
					return fmt.Errorf("parsing mapping file: %w", err)
				}
				for k, v := range mf.Mappings {
					moduleMappings[k] = v
				}
				for k, v := range mf.Modules {
					moduleMappings[k] = v
				}
				for k, v := range mf.Resources {
					resourceMappings[k] = v
				}
			}

			// Read config secrets from stack if --project-dir and --stack are set.
			var configSecrets map[string]string
			if projectDir != "" && stack != "" {
				ctx := context.Background()
				ws, err := auto.NewLocalWorkspace(ctx, auto.WorkDir(projectDir))
				if err != nil {
					return fmt.Errorf("creating workspace: %w", err)
				}
				allConfig, err := ws.GetAllConfig(ctx, stack)
				if err != nil {
					return fmt.Errorf("reading stack config: %w", err)
				}
				configSecrets = make(map[string]string, len(allConfig))
				for key, val := range allConfig {
					if val.Secret {
						// Strip "project:" namespace prefix if present.
						cleanKey := key
						if idx := strings.Index(key, ":"); idx >= 0 {
							cleanKey = key[idx+1:]
						}
						configSecrets[cleanKey] = val.Value
					}
				}
				fmt.Fprintf(os.Stderr, "Loaded %d secret config values from stack %s\n", len(configSecrets), stack)
			}

			// Patch.
			patched, result, err := pkg.PatchState(stateData, &digest, fieldsFile, moduleMappings, resourceMappings, configSecrets, configDir)
			if err != nil {
				return err
			}

			// Write output.
			if err := os.WriteFile(outPath, patched, 0o600); err != nil {
				return fmt.Errorf("writing output: %w", err)
			}

			// Print stats.
			fmt.Fprintf(os.Stderr, "Patched state written to %s\n", outPath)
			fmt.Fprintf(os.Stderr, "  Patched:            %d resources\n", result.Patched)
			fmt.Fprintf(os.Stderr, "  Fields from digest: %d\n", result.FieldsFromDigest)
			fmt.Fprintf(os.Stderr, "  Fields from defaults: %d\n", result.FieldsFromDefaults)
			fmt.Fprintf(os.Stderr, "  Skipped sensitive:  %d\n", result.SkippedSensitive)
			fmt.Fprintf(os.Stderr, "  No fields to patch: %d\n", result.NoFields)
			fmt.Fprintf(os.Stderr, "  Digest mapped:      %d\n", result.DigestMapped)

			_ = strings.TrimSpace // suppress unused import if needed
			return nil
		},
	}

	cmd.Flags().StringVar(&statePath, "state", "", "Exported stack state (from pulumi stack export)")
	cmd.Flags().StringVar(&digestPath, "digest", "", "TF digest (tf-digest.json)")
	cmd.Flags().StringVar(&fieldsPath, "fields", "", "aws-import-diff-fields.json")
	cmd.Flags().StringVar(&mappingFile, "mapping-file", "", "Path to YAML mapping file")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "Output path for patched state")
	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Pulumi project directory (for reading stack config secrets)")
	cmd.Flags().StringVar(&stack, "stack", "", "Pulumi stack name (for reading stack config secrets)")
	cmd.Flags().StringVar(&configDir, "config-dir", "", "TF config directory (for resolving asset file paths)")

	cmd.MarkFlagRequired("state")
	cmd.MarkFlagRequired("digest")
	cmd.MarkFlagRequired("fields")
	cmd.MarkFlagRequired("out")

	return cmd
}

func init() {
	rootCmd.AddCommand(newPatchStateCmd())
}
