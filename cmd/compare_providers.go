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
	"sort"

	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/providermap"
	"github.com/spf13/cobra"
)

const defaultCiMgmtURL = "https://raw.githubusercontent.com/pulumi/ci-mgmt/master/provider-ci/providers.json"

// Providers that are in ci-mgmt but are not bridged Terraform providers
// (they are Pulumi-native, component providers, or special-purpose).
var nonBridgedProviders = map[string]bool{
	"aws-apigateway":           true, // Component provider
	"aws-native":               true, // Pulumi-native (AWS Cloud Control)
	"awsx":                     true, // Component provider
	"command":                  true, // Pulumi-native
	"docker-build":             true, // Component provider
	"eks":                      true, // Component provider
	"kubernetes":               true, // Native provider
	"kubernetes-cert-manager":  true, // Component provider
	"kubernetes-coredns":       true, // Component provider
	"kubernetes-ingress-nginx": true, // Component provider
	"provider-boilerplate":     true, // Template
	"terraform":                true, // Not a provider
	"terraform-module":         true, // Not a provider
	"tf-provider-boilerplate":  true, // Template
	"xyz":                      true, // Template/example
}

func newCompareProvidersCmd() *cobra.Command {
	var ciMgmtURL string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "compare-providers <versions.yaml>",
		Short: "Compare providers in versions.yaml with ci-mgmt providers list",
		Long: `Compare the list of bridged providers in versions.yaml with the official
ci-mgmt providers list from https://github.com/pulumi/ci-mgmt.

This helps identify:
- Providers in ci-mgmt that are missing from versions.yaml
- Providers in versions.yaml that are not in ci-mgmt

By default, missing providers are automatically added to versions.yaml.
Use --dry-run to only compare without modifying the file.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			versionMapPath := args[0]
			return compareProviders(versionMapPath, ciMgmtURL, dryRun)
		},
		Hidden: true, // admin command, not intended for general usage
	}

	cmd.Flags().StringVar(&ciMgmtURL, "ci-mgmt-url", defaultCiMgmtURL,
		"URL to fetch the ci-mgmt providers.json from")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"Only compare providers without modifying versions.yaml")

	return cmd
}

func init() {
	rootCmd.AddCommand(newCompareProvidersCmd())
}

func compareProviders(versionMapPath, ciMgmtURL string, dryRun bool) error {
	vm, err := providermap.LoadVersionMapFromYAML(versionMapPath)
	if err != nil {
		return fmt.Errorf("error loading VersionMap: %w", err)
	}

	var ciMgmtProviders []string
	ciMgmtProviders, err = fetchCiMgmtFromURL(ciMgmtURL)
	if err != nil {
		return fmt.Errorf("error loading ci-mgmt providers: %w", err)
	}

	versionsYamlProviders := make(map[string]bool)
	for bp := range vm.Bridged {
		versionsYamlProviders[string(bp)] = true
	}

	ciMgmtBridged := make(map[string]bool)
	for _, p := range ciMgmtProviders {
		if !nonBridgedProviders[p] {
			ciMgmtBridged[p] = true
		}
	}

	var missingFromVersionsYaml []string
	for p := range ciMgmtBridged {
		if !versionsYamlProviders[p] {
			missingFromVersionsYaml = append(missingFromVersionsYaml, p)
		}
	}
	sort.Strings(missingFromVersionsYaml)

	var notInCiMgmt []string
	for p := range versionsYamlProviders {
		if !ciMgmtBridged[p] {
			notInCiMgmt = append(notInCiMgmt, p)
		}
	}
	sort.Strings(notInCiMgmt)

	fmt.Printf("Comparison Results\n")
	fmt.Printf("==================\n\n")

	fmt.Printf("versions.yaml providers: %d\n", len(versionsYamlProviders))
	fmt.Printf("ci-mgmt bridged providers: %d\n", len(ciMgmtBridged))
	fmt.Printf("ci-mgmt excluded (non-bridged): %d\n\n", len(nonBridgedProviders))

	if len(missingFromVersionsYaml) > 0 {
		fmt.Printf("Providers in ci-mgmt but MISSING from versions.yaml (%d):\n", len(missingFromVersionsYaml))
		for _, p := range missingFromVersionsYaml {
			fmt.Printf("  - %s\n", p)
		}
		fmt.Println()
	} else {
		fmt.Println("All ci-mgmt bridged providers are present in versions.yaml.")
		fmt.Println()
	}

	if len(notInCiMgmt) > 0 {
		fmt.Printf("Providers in versions.yaml but NOT in ci-mgmt (%d):\n", len(notInCiMgmt))
		for _, p := range notInCiMgmt {
			fmt.Printf("  - %s\n", p)
		}
		fmt.Println()
	} else {
		fmt.Println("All versions.yaml providers are present in ci-mgmt.")
		fmt.Println()
	}

	if len(missingFromVersionsYaml) == 0 && len(notInCiMgmt) == 0 {
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

func fetchCiMgmtFromURL(url string) ([]string, error) {
	resp, err := http.Get(url)
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

	var providers []string
	if err := json.Unmarshal(body, &providers); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return providers, nil
}
