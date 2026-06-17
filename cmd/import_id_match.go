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
	"os"
	"strings"

	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newImportIDMatchCmd() *cobra.Command {
	var digestPath string
	var importFilePath string
	var mapFlags []string
	var mappingFile string
	var outPath string

	cmd := &cobra.Command{
		Use:   "import-id-match",
		Short: "Fill Pulumi import file IDs by matching TF digest resources to Pulumi components",
		Long: `Match Terraform resources from a tf-digest.json to Pulumi import file
entries and fill in placeholder import IDs.

The command takes:
  - A TF digest (from "tf-digest" command) containing TF modules and resources
  - A Pulumi import file (from "pulumi preview --import-file") with placeholder IDs
  - Mappings from TF module paths to Pulumi component instance names
  - Optional resource-level mappings from TF addresses to Pulumi resource names

It matches resources by type + name within each mapped module/component pair
and writes a filled import file. Resource-level mappings are applied first for
individual resources that don't follow the naming convention.

Examples:

  # Basic usage with CLI map flags
  pulumi-terraform-migrate import-id-match \
    --digest tf-digest.json \
    --import-file import.json \
    --map 'module.caas_rds=caas_rds' \
    --map 'module.capture_ui["dmvhm"]=capture_ui["dmvhm"]' \
    --out filled-import.json

  # Using a mapping file (supports both module and resource mappings)
  pulumi-terraform-migrate import-id-match \
    --digest tf-digest.json \
    --import-file import.json \
    --mapping-file mappings.yaml \
    --out filled-import.json

  # Mapping file format:
  #   modules:
  #     "module.caas_rds": "caas_rds"
  #   resources:
  #     "aws_s3_bucket.my_bucket": "my_bucket"
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load TF digest.
			digestData, err := os.ReadFile(digestPath)
			if err != nil {
				return fmt.Errorf("reading digest file: %w", err)
			}
			var digest pkg.ModuleMap
			if err := json.Unmarshal(digestData, &digest); err != nil {
				return fmt.Errorf("parsing digest file: %w", err)
			}

			// Load import file.
			importData, err := os.ReadFile(importFilePath)
			if err != nil {
				return fmt.Errorf("reading import file: %w", err)
			}
			var importFile pkg.ImportFile
			if err := json.Unmarshal(importData, &importFile); err != nil {
				return fmt.Errorf("parsing import file: %w", err)
			}

			// Build mappings: start from file, then override with CLI flags.
			moduleMappings := make(map[string]string)
			resourceMappings := make(map[string]string)

			if mappingFile != "" {
				mfData, err := os.ReadFile(mappingFile)
				if err != nil {
					return fmt.Errorf("reading mapping file: %w", err)
				}
				var mf struct {
					Modules   map[string]string `yaml:"modules"`
					Mappings  map[string]string `yaml:"mappings"`  // deprecated alias for modules
					Resources map[string]string `yaml:"resources"`
				}
				if err := yaml.Unmarshal(mfData, &mf); err != nil {
					return fmt.Errorf("parsing mapping file: %w", err)
				}
				// "mappings" is the deprecated key; "modules" takes precedence.
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

			for _, m := range mapFlags {
				parts := strings.SplitN(m, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid --map flag %q: expected format 'module.X=componentName'", m)
				}
				moduleMappings[parts[0]] = parts[1]
			}

			// Run the fill logic (provider fields and nameTable are preserved
			// automatically since ImportEntry has Provider and ImportFile has NameTable).
			result := pkg.FillImportFile(&digest, &importFile, moduleMappings, resourceMappings)

			// Translate TF import IDs to Pulumi-expected formats.
			translated := pkg.TranslateImportIDs(&importFile, &digest)

			// Write output.
			outData, err := json.MarshalIndent(&importFile, "", "    ")
			if err != nil {
				return fmt.Errorf("marshaling output: %w", err)
			}
			if err := os.WriteFile(outPath, outData, 0o644); err != nil {
				return fmt.Errorf("writing output file: %w", err)
			}

			// Print stats to stderr.
			fmt.Fprintf(os.Stderr, "Filled:    %d\n", result.Filled)
			fmt.Fprintf(os.Stderr, "Skipped:   %d (components)\n", result.Skipped)
			fmt.Fprintf(os.Stderr, "Unmatched: %d\n", result.Unmatched)
			if translated > 0 {
				fmt.Fprintf(os.Stderr, "Translated: %d IDs\n", translated)
			}
			for _, w := range result.Warnings {
				fmt.Fprintf(os.Stderr, "  WARNING: %s\n", w)
			}
			fmt.Fprintf(os.Stderr, "Output written to %s\n", outPath)

			return nil
		},
	}

	cmd.Flags().StringVar(&digestPath, "digest", "", "Path to tf-digest.json")
	cmd.Flags().StringVar(&importFilePath, "import-file", "", "Path to Pulumi import file (from pulumi preview --import-file)")
	cmd.Flags().StringArrayVar(&mapFlags, "map", nil, "TF module to Pulumi component mapping (repeatable, format: module.X=componentName)")
	cmd.Flags().StringVar(&mappingFile, "mapping-file", "", "Path to YAML mapping file")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "Output path for the filled import file")

	cmd.MarkFlagRequired("digest")
	cmd.MarkFlagRequired("import-file")
	cmd.MarkFlagRequired("out")

	return cmd
}

func init() {
	rootCmd.AddCommand(newImportIDMatchCmd())
}
