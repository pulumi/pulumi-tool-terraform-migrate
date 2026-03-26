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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"

	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/providermap"
	"github.com/spf13/cobra"
)

const defaultRegistryURL = "https://api.pulumi.com/api/preview/registry/packages"

// Bridged providers not listed in the Pulumi registry API but known to exist.
var additionalBridgedProviders = []string{
	"archive",
	"external",
	"http",
	"null",
}

func newCompareProvidersCmd() *cobra.Command {
	var registryURL string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "compare-providers <versions.yaml>",
		Short: "Compare providers in versions.yaml with Pulumi registry bridged providers",
		Long: `Compare the list of bridged providers in versions.yaml with the bridged
providers listed in the Pulumi registry API.

This helps identify:
- Bridged providers in the registry that are missing from versions.yaml
- Providers in versions.yaml that are not in the registry

By default, missing providers are automatically added to versions.yaml.
Use --dry-run to only compare without modifying the file.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			versionMapPath := args[0]
			return compareProviders(versionMapPath, registryURL, dryRun)
		},
		Hidden: true, // admin command, not intended for general usage
	}

	cmd.Flags().StringVar(&registryURL, "registry-url", defaultRegistryURL,
		"URL for the Pulumi registry API packages endpoint")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"Only compare providers without modifying versions.yaml")

	return cmd
}

func init() {
	rootCmd.AddCommand(newCompareProvidersCmd())
}

type registryPackage struct {
	Name         string   `json:"name"`
	Publisher    string   `json:"publisher"`
	PackageTypes []string `json:"packageTypes"`
	Source       string   `json:"source"`
}

type registryResponse struct {
	Packages          []registryPackage `json:"packages"`
	ContinuationToken string            `json:"continuationToken"`
}

func compareProviders(versionMapPath, registryURL string, dryRun bool) error {
	vm, err := providermap.LoadVersionMapFromYAML(versionMapPath)
	if err != nil {
		return fmt.Errorf("error loading VersionMap: %w", err)
	}

	registryProviders, err := fetchBridgedProvidersFromRegistry(registryURL)
	if err != nil {
		return fmt.Errorf("error loading registry providers: %w", err)
	}

	versionsYamlProviders := make(map[string]bool)
	for bp := range vm.Bridged {
		versionsYamlProviders[string(bp)] = true
	}

	registryBridged := make(map[string]bool)
	for _, p := range registryProviders {
		registryBridged[p] = true
	}
	for _, p := range additionalBridgedProviders {
		registryBridged[p] = true
	}

	var missingFromVersionsYaml []string
	for p := range registryBridged {
		if !versionsYamlProviders[p] {
			missingFromVersionsYaml = append(missingFromVersionsYaml, p)
		}
	}
	sort.Strings(missingFromVersionsYaml)

	var notInRegistry []string
	for p := range versionsYamlProviders {
		if !registryBridged[p] {
			notInRegistry = append(notInRegistry, p)
		}
	}
	sort.Strings(notInRegistry)

	fmt.Printf("Comparison Results\n")
	fmt.Printf("==================\n\n")

	fmt.Printf("versions.yaml providers: %d\n", len(versionsYamlProviders))
	fmt.Printf("Registry bridged providers: %d\n\n", len(registryBridged))

	if len(missingFromVersionsYaml) > 0 {
		fmt.Printf("Providers in registry but MISSING from versions.yaml (%d):\n", len(missingFromVersionsYaml))
		for _, p := range missingFromVersionsYaml {
			fmt.Printf("  - %s\n", p)
		}
		fmt.Println()
	} else {
		fmt.Println("All registry bridged providers are present in versions.yaml.")
		fmt.Println()
	}

	if len(notInRegistry) > 0 {
		fmt.Printf("Providers in versions.yaml but NOT in registry (%d):\n", len(notInRegistry))
		for _, p := range notInRegistry {
			fmt.Printf("  - %s\n", p)
		}
		fmt.Println()
	} else {
		fmt.Println("All versions.yaml providers are present in registry.")
		fmt.Println()
	}

	if len(missingFromVersionsYaml) == 0 && len(notInRegistry) == 0 {
		fmt.Println("Provider lists are in sync!")
		return nil
	}

	if dryRun {
		return fmt.Errorf("provider lists are not in sync (dry-run mode, no changes made)")
	}

	if len(missingFromVersionsYaml) > 0 {
		fmt.Printf("Adding %d missing providers to versions.yaml...\n", len(missingFromVersionsYaml))
		if vm.Bridged == nil {
			vm.Bridged = make(map[providermap.BridgedProvider][]providermap.VersionPair)
		}
		for _, p := range missingFromVersionsYaml {
			vm.Bridged[providermap.BridgedProvider(p)] = []providermap.VersionPair{}
		}
		if err := vm.SaveToYAML(versionMapPath); err != nil {
			return fmt.Errorf("failed to save versions.yaml: %w", err)
		}
		fmt.Printf("Successfully added %d providers to %s\n", len(missingFromVersionsYaml), versionMapPath)
	}

	return nil
}

func fetchBridgedProvidersFromRegistry(registryURL string) ([]string, error) {
	// A provider name may appear multiple times in the registry (e.g. published by
	// both "pulumi" and a third party). We only include providers that have at least
	// one entry published by Pulumi.
	pulumiPublished := make(map[string]bool)
	url := registryURL + "?limit=500"

	for {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.pulumi+8")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch %s: %w", url, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		var result registryResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}

		for _, pkg := range result.Packages {
			if pkg.Source == "pulumi" && pkg.Publisher == "pulumi" && slices.Contains(pkg.PackageTypes, "bridged") {
				pulumiPublished[pkg.Name] = true
			}
		}

		if result.ContinuationToken == "" {
			break
		}
		url = registryURL + "?limit=500&continuationToken=" + result.ContinuationToken
	}

	providers := make([]string, 0, len(pulumiPublished))
	for name := range pulumiPublished {
		providers = append(providers, name)
	}
	sort.Strings(providers)
	return providers, nil
}
