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
	"os"
	"path/filepath"
	"sort"
	"strings"

	goversion "github.com/hashicorp/go-version"
	disco "github.com/opentofu/svchost/disco"
	"github.com/pulumi/opentofu/configs"
	bridgeAddrs "github.com/pulumi/pulumi-terraform-bridge/v3/pkg/vendored/opentofu/addrs"
	bridgeConfigSchema "github.com/pulumi/pulumi-terraform-bridge/v3/pkg/vendored/opentofu/configs/configschema"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/vendored/opentofu/getproviders"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tfprovider"
)

// SensitivityMap maps resource types to their sensitive attribute names.
// e.g. {"aws_secretsmanager_secret_version": {"secret_string": true}}
type SensitivityMap map[string]map[string]bool

// BuildSensitivityMap loads provider schemas and builds a map of which
// attributes are marked as sensitive for each resource type.
//
// It resolves provider version constraints from the config, downloads
// providers via the registry, and queries their schemas via gRPC.
func BuildSensitivityMap(ctx context.Context, config *configs.Config) (SensitivityMap, error) {
	if config == nil || config.Module == nil || config.Module.ProviderRequirements == nil {
		return nil, nil
	}

	sm := make(SensitivityMap)
	registryDisco := disco.New()

	for _, req := range config.Module.ProviderRequirements.RequiredProviders {
		// Skip the built-in terraform provider.
		if req.Type.IsBuiltIn() {
			continue
		}

		// Resolve the version constraint to an exact version.
		version, err := resolveProviderVersion(ctx, req.Type.String(), req.Requirement.Required, registryDisco)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not resolve version for provider %s: %v\n", req.Type, err)
			continue
		}

		// Check disk cache first.
		if cached, err := readCachedSensitivity(req.Type.String(), version); err == nil {
			fmt.Fprintf(os.Stderr, "Using cached sensitivity map for %s %s\n", req.Type, version)
			for k, v := range cached {
				sm[k] = v
			}
			continue
		}

		fmt.Fprintf(os.Stderr, "Loading provider schema for %s %s...\n", req.Type, version)

		// Load the provider.
		provider, err := tfprovider.LoadProvider(ctx, req.Type.String(), version)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load provider %s: %v\n", req.Type, err)
			continue
		}

		// Get the schema.
		schemaResp := provider.GetProviderSchema(ctx)
		if schemaResp.Diagnostics.HasErrors() {
			fmt.Fprintf(os.Stderr, "Warning: could not get schema for provider %s: %v\n",
				req.Type, schemaResp.Diagnostics.Err())
			provider.Close(ctx) //nolint:errcheck
			continue
		}

		// Walk resource schemas and record sensitive attributes.
		providerSensitivity := make(SensitivityMap)
		for resourceType, resourceSchema := range schemaResp.ResourceTypes {
			sensitiveAttrs := findSensitiveAttributes(resourceSchema.Block, "")
			if len(sensitiveAttrs) > 0 {
				providerSensitivity[resourceType] = sensitiveAttrs
				sm[resourceType] = sensitiveAttrs
			}
		}

		provider.Close(ctx) //nolint:errcheck

		// Write to disk cache for next time.
		if err := writeCachedSensitivity(req.Type.String(), version, providerSensitivity); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not cache sensitivity map for %s: %v\n", req.Type, err)
		}
	}

	return sm, nil
}

// resolveProviderVersion queries the registry for available versions matching
// the constraint and returns the latest matching version string.
func resolveProviderVersion(
	ctx context.Context,
	providerAddr string,
	constraints goversion.Constraints,
	registryDisco *disco.Disco,
) (string, error) {
	bridgeAddr, diags := bridgeAddrs.ParseProviderSourceString(providerAddr)
	if diags.HasErrors() {
		return "", fmt.Errorf("parsing provider address %s: %s", providerAddr, diags.Err())
	}

	source := getproviders.NewRegistrySource(ctx, registryDisco, nil, getproviders.LocationConfig{})
	available, _, err := source.AvailableVersions(ctx, bridgeAddr)
	if err != nil {
		return "", fmt.Errorf("querying available versions for %s: %w", providerAddr, err)
	}

	if len(available) == 0 {
		return "", fmt.Errorf("no versions available for %s", providerAddr)
	}

	// Sort descending to find latest matching version.
	sort.Sort(sort.Reverse(available))

	for _, v := range available {
		gv, err := goversion.NewVersion(v.String())
		if err != nil {
			continue
		}
		if constraints == nil || constraints.Check(gv) {
			return v.String(), nil
		}
	}

	// If no constraint matches, use the latest.
	return available[0].String(), nil
}

// findSensitiveAttributes walks a provider schema block and returns a map
// of attribute names that are marked as Sensitive.
func findSensitiveAttributes(block *bridgeConfigSchema.Block, prefix string) map[string]bool {
	result := make(map[string]bool)
	if block == nil {
		return result
	}

	for name, attr := range block.Attributes {
		fullName := name
		if prefix != "" {
			fullName = prefix + "." + name
		}
		if attr.Sensitive {
			result[fullName] = true
		}
	}

	for name, nested := range block.BlockTypes {
		fullName := name
		if prefix != "" {
			fullName = prefix + "." + name
		}
		for k, v := range findSensitiveAttributes(&nested.Block, fullName) {
			result[k] = v
		}
	}

	return result
}

// sensitivityCacheDir returns the path to the sensitivity cache directory.
func sensitivityCacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pulumi", "sensitivity-cache")
}

// sensitivityCacheKey builds a filename-safe cache key from a provider address and version.
func sensitivityCacheKey(providerAddr, version string) string {
	safe := strings.ReplaceAll(providerAddr, "/", "-")
	return safe + "@" + version + ".json"
}

// readCachedSensitivity reads a cached sensitivity map for a provider+version from disk.
func readCachedSensitivity(providerAddr, version string) (SensitivityMap, error) {
	dir := sensitivityCacheDir()
	if dir == "" {
		return nil, fmt.Errorf("could not determine cache directory")
	}
	path := filepath.Join(dir, sensitivityCacheKey(providerAddr, version))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sm SensitivityMap
	if err := json.Unmarshal(data, &sm); err != nil {
		return nil, err
	}
	return sm, nil
}

// writeCachedSensitivity writes a sensitivity map for a provider+version to disk.
func writeCachedSensitivity(providerAddr, version string, sm SensitivityMap) error {
	dir := sensitivityCacheDir()
	if dir == "" {
		return fmt.Errorf("could not determine cache directory")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(sm)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, sensitivityCacheKey(providerAddr, version)), data, 0644)
}

// RedactSensitiveAttributes returns a copy of attrs with sensitive values
// replaced by "(sensitive)".
func RedactSensitiveAttributes(attrs map[string]interface{}, sensitiveFields map[string]bool) map[string]interface{} {
	if len(sensitiveFields) == 0 {
		return attrs
	}

	result := make(map[string]interface{}, len(attrs))
	for k, v := range attrs {
		if sensitiveFields[k] {
			result[k] = "(sensitive)"
		} else {
			result[k] = v
		}
	}
	return result
}
