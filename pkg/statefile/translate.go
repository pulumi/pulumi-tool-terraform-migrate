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

// TranslateResource converts a Terraform statefile resource to Pulumi format.
// This works directly with OpenTofu's states.Resource type (the native state format)
// instead of the intermediate tfjson.StateResource representation.
//
// Known limitations:
//   - State schema upgrades are not performed. If the statefile contains resources
//     created with an older provider version (e.g., schema_version 1), attributes
//     may differ from the current provider schema (e.g., "number" vs "numeric" in
//     the random provider). Policies written against newer schema attributes may
//     not match older state.
//
// Returns a pkg.PulumiResource with translated properties, or an error if conversion fails.
func TranslateResource(
	res *states.Resource,
	key addrs.InstanceKey,
	providers map[providermap.TerraformProviderName]*info.Provider,
) (pkg.PulumiResource, error) {
	instance := res.Instances[key]
	if instance == nil || instance.Current == nil {
		return pkg.PulumiResource{}, fmt.Errorf("no current instance found for key %v", key)
	}

	// Extract fields from the resource and instance
	address := res.Addr.Instance(key).String()
	attrsJSON := instance.Current.AttrsJSON
	sensitivePaths := instance.Current.AttrSensitivePaths
	providerName := res.ProviderConfig.Provider.String()
	resourceType := res.Addr.Resource.Type

	prov, ok := providers[providermap.TerraformProviderName(providerName)]
	if !ok {
		return pkg.PulumiResource{}, fmt.Errorf("no Pulumi provider found for Terraform provider: %s", providerName)
	}
	shimResource := prov.P.ResourcesMap().Get(resourceType)
	if shimResource == nil {
		return pkg.PulumiResource{}, fmt.Errorf("no resource type found for Terraform resource: %s", resourceType)
	}

	// Convert AttrsJSON directly to cty.Value using the schema type
	ctyType := bridge.ImpliedType(shimResource.Schema(), true)
	ctyValue, err := ctyjson.Unmarshal(attrsJSON, ctyType)
	if err != nil {
		return pkg.PulumiResource{}, fmt.Errorf("failed to convert attrs JSON to CTY value: %w", err)
	}

	// Extract cty.Path directly from PathValueMarks (no JSON round-trip!)
	sensitiveCtyPaths := make([]cty.Path, len(sensitivePaths))
	for i, pvm := range sensitivePaths {
		sensitiveCtyPaths[i] = pvm.Path
	}

	pulumiTypeToken, err := bridge.PulumiTypeToken(resourceType, prov)
	if err != nil {
		return pkg.PulumiResource{}, fmt.Errorf("failed to get Pulumi type token: %w", err)
	}
	resourceInfo := prov.Resources[resourceType]
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
			Name: pkg.PulumiNameFromTerraformAddress(address, resourceType),
			Type: string(pulumiTypeToken),
		},
		Inputs:  inputs,
		Outputs: props,
	}, nil
}
