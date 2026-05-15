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

package pkg

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pulumi/opentofu/states"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/bridgedproviders"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/providermap"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tofu"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
)

func getTerraformProvidersForTerraformState(tfState *tfjson.State) ([]providermap.TerraformProviderName, error) {
	tfProviders := map[providermap.TerraformProviderName]struct{}{}

	err := tofu.VisitResources(tfState, func(resource *tfjson.StateResource) error {
		tfProviders[providermap.TerraformProviderName(resource.ProviderName)] = struct{}{}
		return nil
	}, &tofu.VisitOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to visit resources: %w", err)
	}

	providers := slices.Collect(maps.Keys(tfProviders))

	return providers, nil
}

func getTerraformProvidersForRawState(state *states.State) []providermap.TerraformProviderName {
	providers := map[providermap.TerraformProviderName]struct{}{}
	for _, module := range state.Modules {
		for _, res := range module.Resources {
			providers[providermap.TerraformProviderName(res.ProviderConfig.Provider.String())] = struct{}{}
		}
	}
	return slices.Collect(maps.Keys(providers))
}

// ProviderWithMetadata wraps a Pulumi provider info with additional metadata
// about how the provider was bridged.
type ProviderWithMetadata struct {
	// Provider is the Pulumi provider info from the bridge.
	*info.Provider
	// IsDynamic is true if this provider was dynamically bridged using the
	// terraform-provider package, rather than a statically bridged provider.
	IsDynamic bool
	// TerraformAddress is the full Terraform provider address (e.g., "registry.terraform.io/hashicorp/time").
	// This is set for all providers, but is primarily useful for dynamic providers
	// to construct the proper package name.
	TerraformAddress string
}

func PulumiProvidersForTerraformProviders(
	terraformProviders []providermap.TerraformProviderName,
	providerVersions map[string]string,
) (map[providermap.TerraformProviderName]*ProviderWithMetadata, error) {
	pulumiProviders := make(map[providermap.TerraformProviderName]*ProviderWithMetadata)

	for _, providerName := range terraformProviders {
		// Get the version for this provider from the version map
		version := ""
		if providerVersions != nil {
			version = providerVersions[string(providerName)]
		}

		pulumiProvider := providermap.RecommendPulumiProvider(providermap.TerraformProvider{
			Identifier: providermap.TerraformProviderName(providerName),
			Version:    version,
		})

		var providerInfo *info.Provider
		var isDynamic bool
		var err error

		if pulumiProvider.StaticallyBridgedProvider != nil {
			providerInfo, err = getMappingFromStaticallyBridgedProvider(pulumiProvider.StaticallyBridgedProvider, providerName)
			if err != nil {
				return nil, err
			}
			isDynamic = false
		} else {
			providerInfo, err = bridgedproviders.GetMappingForTerraformProvider(
				context.Background(),
				string(providerName),
				version,
			)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to dynamically bridge provider %s: %v\n", providerName, err)
				fmt.Fprintf(os.Stderr, "Warning: resources using provider %s will be skipped\n", providerName)
				continue
			}
			isDynamic = true
		}

		pulumiProviders[providerName] = &ProviderWithMetadata{
			Provider:         providerInfo,
			IsDynamic:        isDynamic,
			TerraformAddress: string(providerName),
		}
	}
	return pulumiProviders, nil
}

func getMappingFromStaticallyBridgedProvider(
	staticProvider *providermap.BridgedPulumiProvider,
	tfProviderName providermap.TerraformProviderName,
) (*info.Provider, error) {
	// Check mapping cache first.
	cacheKey := staticProvider.Identifier + "@" + staticProvider.Version
	if cached, err := readCachedMapping(cacheKey); err == nil {
		fmt.Fprintf(os.Stderr, "Using cached mapping for %s %s\n", staticProvider.Identifier, staticProvider.Version)
		providerInfo, err := bridgedproviders.UnmarshalMappingData(cached)
		if err == nil {
			return providerInfo, nil
		}
		// Cache corrupt — fall through to fresh load.
		fmt.Fprintf(os.Stderr, "Warning: cached mapping for %s is corrupt, reloading: %v\n", cacheKey, err)
	}

	fmt.Fprintf(os.Stderr, "Loading mapping for %s %s...\n", staticProvider.Identifier, staticProvider.Version)

	result, err := bridgedproviders.EnsureProviderInstalled(context.Background(), bridgedproviders.InstallProviderOptions{
		Name:    staticProvider.Identifier,
		Version: staticProvider.Version,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to install provider %s: %w", tfProviderName, err)
	}

	mapping, err := bridgedproviders.GetMappingFromBinary(context.Background(), result.BinaryPath, bridgedproviders.GetMappingOptions{
		Key:      "terraform",
		Provider: staticProvider.Identifier,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get mapping for provider %s: %w", tfProviderName, err)
	}

	// Write to cache for next time.
	if err := writeCachedMapping(cacheKey, mapping); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not cache mapping for %s: %v\n", cacheKey, err)
	}

	providerInfo, err := bridgedproviders.UnmarshalMappingData(mapping)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal mapping for provider %s: %w", tfProviderName, err)
	}

	return providerInfo, nil
}

// mappingCacheDir returns the path to the mapping cache directory.
func mappingCacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pulumi", "mapping-cache")
}

// mappingCacheFileName builds a filename-safe cache key.
func mappingCacheFileName(key string) string {
	safe := strings.ReplaceAll(key, "/", "-")
	return safe + ".json"
}

// readCachedMapping reads a cached GetMappingResult from disk.
func readCachedMapping(key string) (*bridgedproviders.GetMappingResult, error) {
	dir := mappingCacheDir()
	if dir == "" {
		return nil, fmt.Errorf("could not determine cache directory")
	}
	data, err := os.ReadFile(filepath.Join(dir, mappingCacheFileName(key)))
	if err != nil {
		return nil, err
	}
	var result bridgedproviders.GetMappingResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// writeCachedMapping writes a GetMappingResult to disk.
func writeCachedMapping(key string, result *bridgedproviders.GetMappingResult) error {
	dir := mappingCacheDir()
	if dir == "" {
		return fmt.Errorf("could not determine cache directory")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, mappingCacheFileName(key)), data, 0644)
}

func GetPulumiProvidersForTerraformState(tfState *tfjson.State, providerVersions map[string]string) (map[providermap.TerraformProviderName]*ProviderWithMetadata, error) {
	// TODO[pulumi/pulumi-service#35512]: This assumes one Pulumi provider per Terraform provider. This means that provider aliases are not supported.
	terraformProviders, err := getTerraformProvidersForTerraformState(tfState)
	if err != nil {
		return nil, fmt.Errorf("failed to get terraform providers: %w", err)
	}
	return PulumiProvidersForTerraformProviders(terraformProviders, providerVersions)
}

func GetProviderInputs(providerName string) (resource.PropertyMap, error) {
	// TODO[pulumi/pulumi-service#35411]: produce correct provider inputs or fail gracefully with instructions
	return resource.PropertyMap{}, nil
}
