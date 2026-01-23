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
	"sync"

	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/providermap"
	"github.com/spf13/cobra"
)

func newUpdateProvidermapCmd() *cobra.Command {
	var provider string
	var recompute bool
	var parallel int

	cmd := &cobra.Command{
		Use:   "update-providermap <versions.yaml>",
		Short: "Update provider version mappings",
		Long: `Update provider version mappings between Terraform and Pulumi providers.

This is an administrative command used to maintain the provider version mapping data.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			versionMapPath := args[0]
			updateProviderMap(versionMapPath, provider, recompute, parallel)
			return nil
		},
		Hidden: true, // admin command, not intended for general usage
	}

	cmd.Flags().StringVar(&provider, "provider", "", "Only update the specified provider (e.g., 'random')")
	cmd.Flags().BoolVar(&recompute, "recompute", false, "Recompute the suggested versions")
	cmd.Flags().IntVar(&parallel, "parallel", 1, "Process providers in parallel")

	return cmd
}

func init() {
	rootCmd.AddCommand(newUpdateProvidermapCmd())
}

func updateProviderMap(versionMapPath string, provider string, recompute bool, parallel int) {
	// Load the VersionMap from YAML
	vm, err := providermap.LoadVersionMapFromYAML(versionMapPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading VersionMap: %v\n", err)
		os.Exit(1)
	}

	// Determine which providers to process
	var providersToProcess []providermap.BridgedProvider
	if provider != "" {
		// If a specific provider is requested, process only that one
		providersToProcess = []providermap.BridgedProvider{providermap.BridgedProvider(provider)}
	} else {
		// Otherwise, process all providers in vm.Bridged
		for bp := range vm.Bridged {
			providersToProcess = append(providersToProcess, bp)
		}
	}

	// Helper function to process a single provider
	processProvider := func(bp providermap.BridgedProvider, mu *sync.Mutex) {
		fmt.Printf("Processing provider: %s\n", bp)

		// Fetch actual tags for the provider
		tags := providermap.FetchReleaseVersions(bp)
		if tags == nil {
			fmt.Fprintf(os.Stderr, "  Warning: failed to fetch tags for %s\n", bp)
			return
		}

		// For every tag not yet in the VersionMap, try to infer upstream version
		for _, tag := range tags {
			var hasVersion bool
			if mu != nil {
				mu.Lock()
				hasVersion = vm.HasPulumiVersion(bp, tag)
				mu.Unlock()
			} else {
				hasVersion = vm.HasPulumiVersion(bp, tag)
			}

			if hasVersion && !recompute {
				continue
			}

			upstreamVersion, err := providermap.InferUpstreamVersion(bp, tag)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  %s: %v\n", tag, err)
				if mu != nil {
					mu.Lock()
					vm.AddError(bp, tag, err.Error())
					mu.Unlock()
				} else {
					vm.AddError(bp, tag, err.Error())
				}
				continue
			}

			if mu != nil {
				mu.Lock()
				vm.AddVersion(bp, tag, upstreamVersion)
				mu.Unlock()
			} else {
				vm.AddVersion(bp, tag, upstreamVersion)
			}
			fmt.Printf("  %s -> %s\n", tag, upstreamVersion)

			// Write the updated VersionMap to YAML after each version (sequential only)
			if parallel <= 1 {
				if err := vm.SaveToYAML(versionMapPath); err != nil {
					fmt.Fprintf(os.Stderr, "Error saving VersionMap: %v\n", err)
					os.Exit(1)
				}
			}
		}
	}

	// Iterate over the providers to process
	if parallel > 1 {
		// Parallel processing with worker pool
		var wg sync.WaitGroup
		var mu sync.Mutex
		providerChan := make(chan providermap.BridgedProvider, len(providersToProcess))

		// Start N workers
		for i := 0; i < parallel; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for bp := range providerChan {
					processProvider(bp, &mu)
				}
			}()
		}

		// Send providers to workers
		for _, bp := range providersToProcess {
			providerChan <- bp
		}
		close(providerChan)

		// Wait for all workers to finish
		wg.Wait()
	} else {
		// Sequential processing
		for _, bp := range providersToProcess {
			processProvider(bp, nil)
		}
	}

	// Write the updated VersionMap to YAML
	if err := vm.SaveToYAML(versionMapPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving VersionMap: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("VersionMap updated successfully.")
}
