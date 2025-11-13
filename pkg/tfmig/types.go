package tfmig

import (
	"context"
	"fmt"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pulumi/pulumi-terraform-migrate/pkg/pulumix"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
)

// TypeMapper maps Terraform resource types to Pulumi resource types.
// It caches provider information to avoid redundant work.
type TypeMapper struct {
	// Cache for GetMappingResult keyed by Pulumi provider name
	mappingCache map[string]*pulumix.GetMappingResult

	// Cache for unmarshaled ProviderInfo keyed by Pulumi provider name
	providerInfoCache map[string]*tfbridge.ProviderInfo

	// Cache for installed provider binary paths keyed by "provider@version"
	binaryPathCache map[string]string
}

// NewTypeMapper creates a new TypeMapper with initialized caches.
func NewTypeMapper() *TypeMapper {
	return &TypeMapper{
		mappingCache:      make(map[string]*pulumix.GetMappingResult),
		providerInfoCache: make(map[string]*tfbridge.ProviderInfo),
		binaryPathCache:   make(map[string]string),
	}
}

// PulumiResourceType returns the Pulumi type token for a given Terraform provider and resource type.
// It caches provider information to minimize repeated installations and lookups.
func (tm *TypeMapper) PulumiResourceType(ctx context.Context, tfProviderName, tfType string) (tokens.Type, error) {
	// Map the Terraform provider to Pulumi provider
	pulumiProvider := GetPulumiProvider(tfProviderName)
	if pulumiProvider == "" {
		return "", fmt.Errorf("no Pulumi provider mapping found for %s", tfProviderName)
	}

	// Get the provider info (from cache or by fetching)
	providerInfo, err := tm.getProviderInfo(ctx, pulumiProvider)
	if err != nil {
		return "", err
	}

	// Look up the Pulumi token for this resource type
	resourceInfo, ok := providerInfo.Resources[tfType]
	if !ok {
		return "", fmt.Errorf("no mapping found for resource type %s in provider %s",
			tfType, pulumiProvider)
	}

	return resourceInfo.Tok, nil
}

// PulumiResourceTypeForState is a convenience method that extracts the provider and type
// from a Terraform state resource and returns the Pulumi type token.
func (tm *TypeMapper) PulumiResourceTypeForState(ctx context.Context, state tfjson.StateResource) (tokens.Type, error) {
	return tm.PulumiResourceType(ctx, state.ProviderName, state.Type)
}

// getProviderInfo retrieves the provider info for a Pulumi provider, using cache when available.
func (tm *TypeMapper) getProviderInfo(ctx context.Context, pulumiProvider string) (*tfbridge.ProviderInfo, error) {
	// Check if we already have provider info cached
	if info, ok := tm.providerInfoCache[pulumiProvider]; ok {
		return info, nil
	}

	// Get the mapping result (from cache or by fetching)
	mappingResult, err := tm.getMappingResult(ctx, pulumiProvider)
	if err != nil {
		return nil, err
	}

	// Unmarshal the mapping data to ProviderInfo
	providerInfo, err := pulumix.UnmarshalMappingData(mappingResult)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal mapping data for %s: %w", pulumiProvider, err)
	}

	// Cache the result
	tm.providerInfoCache[pulumiProvider] = providerInfo
	return providerInfo, nil
}

// getMappingResult retrieves the mapping result for a Pulumi provider, using cache when available.
func (tm *TypeMapper) getMappingResult(ctx context.Context, pulumiProvider string) (*pulumix.GetMappingResult, error) {
	// Check if we already have mapping cached
	if result, ok := tm.mappingCache[pulumiProvider]; ok {
		return result, nil
	}

	// Get the provider binary path (installing if necessary)
	binaryPath, err := tm.getProviderBinaryPath(ctx, pulumiProvider)
	if err != nil {
		return nil, err
	}

	// Get mapping from the installed provider
	mappingResult, err := pulumix.GetMappingFromBinary(ctx, binaryPath, pulumix.GetMappingOptions{
		Key:      "terraform",
		Provider: pulumiProvider,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get mapping for %s: %w", pulumiProvider, err)
	}

	// Cache the result
	tm.mappingCache[pulumiProvider] = mappingResult
	return mappingResult, nil
}

// getProviderBinaryPath retrieves the binary path for a Pulumi provider, installing if necessary.
func (tm *TypeMapper) getProviderBinaryPath(ctx context.Context, pulumiProvider string) (string, error) {
	version := GetProviderVersion(pulumiProvider)
	cacheKey := fmt.Sprintf("%s@%s", pulumiProvider, version)

	// Check if we already have the binary path cached
	if path, ok := tm.binaryPathCache[cacheKey]; ok {
		return path, nil
	}

	// Try to find an existing installation
	binaryPath, err := pulumix.GetInstalledProviderPath(ctx, pulumiProvider, version, "")
	if err != nil {
		// Provider not installed, install it
		installResult, err := pulumix.InstallProvider(ctx, pulumix.InstallProviderOptions{
			Name:    pulumiProvider,
			Version: version,
		})
		if err != nil {
			return "", fmt.Errorf("failed to install provider %s: %w", pulumiProvider, err)
		}
		binaryPath = installResult.BinaryPath
	}

	// Cache the binary path
	tm.binaryPathCache[cacheKey] = binaryPath
	return binaryPath, nil
}
