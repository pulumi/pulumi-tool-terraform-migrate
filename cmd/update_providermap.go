// Copyright 2016-2026, Pulumi Corporation.
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

	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/providermap"
	"github.com/spf13/cobra"
)

func newUpdateProvidermapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-providermap <versions.yaml>",
		Short: "Update provider version mappings",
		Long: `Update provider version mappings between Terraform and Pulumi providers.

This is an administrative command used to maintain the provider version mapping data.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			versionMapPath := args[0]
			updateProviderMap(versionMapPath)
			return nil
		},
	}

	return cmd
}

func init() {
	// Only register this command if PULUMI_ADMIN_COMMANDS=true
	if os.Getenv("PULUMI_ADMIN_COMMANDS") == "true" {
		rootCmd.AddCommand(newUpdateProvidermapCmd())
	}
}

func updateProviderMap(versionMapPath string) {
	// Load the VersionMap from YAML
	vm, err := providermap.LoadVersionMapFromYAML(versionMapPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading VersionMap: %v\n", err)
		os.Exit(1)
	}

	// Iterate over every bridged provider
	for bp := range vm.Bridged {
		fmt.Printf("Processing provider: %s\n", bp)

		// Fetch actual tags for the provider
		tags := providermap.FetchReleaseVersions(bp)
		if tags == nil {
			fmt.Fprintf(os.Stderr, "  Warning: failed to fetch tags for %s\n", bp)
			continue
		}

		// For every tag not yet in the VersionMap, try to infer upstream version
		for _, tag := range tags {
			if vm.HasPulumiVersion(bp, tag) {
				continue
			}

			upstreamVersion, err := providermap.InferUpstreamVersion(bp, tag)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  %s: %v\n", tag, err)
				vm.AddError(bp, tag, err.Error())
				continue
			}

			vm.AddVersion(bp, tag, upstreamVersion)
			fmt.Printf("  %s -> %s\n", tag, upstreamVersion)
		}
	}

	// Write the updated VersionMap to YAML
	if err := vm.SaveToYAML(versionMapPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving VersionMap: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("VersionMap updated successfully.")
}
