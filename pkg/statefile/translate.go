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

// Package statefile provides functions for translating Terraform statefile resources
// to Pulumi format. This package works directly with OpenTofu's states.Resource type,
// avoiding the intermediate tfjson.StateResource representation.
package statefile

import (
	"context"
	"fmt"

	"github.com/pulumi/opentofu/addrs"
	"github.com/pulumi/opentofu/states"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/bridge"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/providermap"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

// TranslateResourceInstance converts a Terraform resource instance to Pulumi format.
// This is a strict translation that requires the instance's attributes to match the provider's current schema.
// Use StateUpgrader.UpgradeInstance first if the state may have an older schema version.
// Returns a pkg.PulumiResource with translated properties, or an error if conversion fails.
func TranslateResourceInstance(
	res *states.Resource,
	key addrs.InstanceKey,
	provider *info.Provider,
) (pkg.PulumiResource, error) {
	instance := res.Instance(key)
	if instance == nil || instance.Current == nil {
		return pkg.PulumiResource{}, fmt.Errorf("no current instance found for key %v", key)
	}

	resourceType := res.Addr.Resource.Type
	shimResource := provider.P.ResourcesMap().Get(resourceType)
	if shimResource == nil {
		return pkg.PulumiResource{}, fmt.Errorf("no resource type found for Terraform resource: %s", resourceType)
	}

	// Convert AttrsJSON directly to cty.Value using the schema type
	ctyType := bridge.ImpliedType(shimResource.Schema(), true)
	ctyValue, err := ctyjson.Unmarshal(instance.Current.AttrsJSON, ctyType)
	if err != nil {
		return pkg.PulumiResource{}, fmt.Errorf("failed to unmarshal attrs JSON: %w", err)
	}

	sensitiveCtyPaths := make([]cty.Path, len(instance.Current.AttrSensitivePaths))
	for i, pvm := range instance.Current.AttrSensitivePaths {
		sensitiveCtyPaths[i] = pvm.Path
	}

	pulumiTypeToken, err := bridge.PulumiTypeToken(resourceType, provider)
	if err != nil {
		return pkg.PulumiResource{}, fmt.Errorf("failed to get Pulumi type token: %w", err)
	}

	resourceInfo := provider.Resources[resourceType]
	props, err := pkg.ConvertTFValueToPulumiValue(ctyValue, shimResource, resourceInfo, sensitiveCtyPaths)
	if err != nil {
		return pkg.PulumiResource{}, fmt.Errorf("failed to convert value to Pulumi value: %w", err)
	}

	inputs, err := tfbridge.ExtractInputsFromOutputs(resource.PropertyMap{}, props, shimResource.Schema(), resourceInfo.Fields, false)
	if err != nil {
		return pkg.PulumiResource{}, fmt.Errorf("failed to extract inputs from outputs: %w", err)
	}

	return pkg.PulumiResource{
		PulumiResourceID: pkg.PulumiResourceID{
			ID:   props["id"].StringValue(),
			Name: pkg.PulumiNameFromTerraformAddress(res.Addr.Instance(key).String(), resourceType),
			Type: string(pulumiTypeToken),
		},
		Inputs:  inputs,
		Outputs: props,
	}, nil
}

// TranslateResult contains the results of translating a Terraform statefile.
type TranslateResult struct {
	// Resources contains successfully translated resources.
	Resources []pkg.PulumiResource
	// Skipped contains resources that could not be translated.
	Skipped []SkippedResource
}

// SkippedResource represents a resource that could not be translated.
type SkippedResource struct {
	Address      string
	ResourceType string
	Provider     string
	Reason       string
}

// TranslateStateFile translates all resources in a Terraform statefile to Pulumi format.
// This handles the complete translation flow including:
//   - Looking up the appropriate Pulumi provider for each resource
//   - Upgrading state via TF provider when schema version is older than current
//
// The function manages the TF provider lifecycle internally - providers are loaded
// lazily when needed for upgrades and cleaned up when the function returns.
//
// Resources that cannot be translated (no matching provider, schema mismatch even after
// upgrade attempt) are reported in the Skipped field of the result.
func TranslateStateFile(
	ctx context.Context,
	sf *states.State,
	providers map[providermap.TerraformProviderName]*info.Provider,
) (*TranslateResult, error) {
	if sf == nil {
		return &TranslateResult{}, nil
	}

	// Find the upstream TF provider version for each bridged provider.
	tfVersions := make(map[string]string)
	for tfName, prov := range providers {
		if version, ok := providermap.GetUpstreamVersion(tfName, prov.Version); ok {
			tfVersions[string(tfName)] = version
		}
	}

	upgrader := NewStateUpgrader(tfVersions)
	defer upgrader.Close()

	result := &TranslateResult{}

	for _, module := range sf.Modules {
		for _, res := range module.Resources {
			providerName := res.ProviderConfig.Provider.String()
			resourceType := res.Addr.Resource.Type

			// Skip data sources - not yet supported
			if res.Addr.Resource.Mode == addrs.DataResourceMode {
				for key := range res.Instances {
					result.Skipped = append(result.Skipped, SkippedResource{
						Address:      res.Addr.Instance(key).String(),
						ResourceType: resourceType,
						Provider:     providerName,
						Reason:       "data sources are not yet supported",
					})
				}
				continue
			}

			provider, ok := providers[providermap.TerraformProviderName(providerName)]
			if !ok {
				// Skip resources without a matching Pulumi provider
				for key := range res.Instances {
					result.Skipped = append(result.Skipped, SkippedResource{
						Address:      res.Addr.Instance(key).String(),
						ResourceType: resourceType,
						Provider:     providerName,
						Reason:       fmt.Sprintf("no Pulumi provider found for Terraform provider: %s", providerName),
					})
				}
				continue
			}

			for key, instance := range res.Instances {
				if instance == nil || instance.Current == nil {
					continue
				}

				address := res.Addr.Instance(key).String()

				// Try translation first. If it fails, attempt upgrade via TF provider.
				// TODO: Consider always upgrading when state schema version differs from
				// provider schema version. Currently the bridged provider shim doesn't
				// reliably expose schema versions (often returns 0), so we fall back to
				// upgrade-on-error.
				translated, err := TranslateResourceInstance(res, key, provider)
				if err != nil {
					upgradedInstance, upgradeErr := upgrader.UpgradeInstance(ctx, res, key)
					if upgradeErr == nil && upgradedInstance != nil {
						instance.Current = upgradedInstance
						translated, err = TranslateResourceInstance(res, key, provider)
					}
				}

				if err != nil {
					result.Skipped = append(result.Skipped, SkippedResource{
						Address:      address,
						ResourceType: resourceType,
						Provider:     providerName,
						Reason:       err.Error(),
					})
					continue
				}

				result.Resources = append(result.Resources, translated)
			}
		}
	}

	return result, nil
}
