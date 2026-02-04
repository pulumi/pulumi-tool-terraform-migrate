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

package bridgedproviders

import (
	"context"
	"fmt"

	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/plugin"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/contract"
)

const TerraformProviderPluginVersion = "v1.1.0"


// GetMappingForTerraformProvider performs dynamic bridging for an arbitrary Terraform provider
// using the terraform-provider Pulumi plugin.
//
// This function:
//
// 1. Ensures the terraform-provider plugin is installed
// 2. Loads the plugin and calls Parameterize with the TF provider address
// 3. Calls GetMapping to retrieve the provider mapping data
// 4. Returns the unmarshalled ProviderInfo
//
// The tfProviderAddr should be a Terraform provider address like:
//   - "registry.terraform.io/hashicorp/random"
//
// The tfProviderVersion should be the version of the TF provider (e.g., "3.6.0").
//
// See https://www.pulumi.com/registry/packages/terraform-provider/ for more details.
func GetMappingForTerraformProvider(
	ctx context.Context,
	tfProviderAddr string,
	tfProviderVersion string,
) (*info.Provider, error) {
	installResult, err := EnsureProviderInstalled(ctx, InstallProviderOptions{
		Name:    "terraform-provider",
		Version: TerraformProviderPluginVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to install terraform-provider plugin: %w", err)
	}

	host := &minimalHost{}
	pctx, err := plugin.NewContext(ctx, nil, nil, nil, nil, "", nil, false, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create plugin context: %w", err)
	}
	defer func() {
		contract.IgnoreError(pctx.Close())
	}()

	provider, err := plugin.NewProviderFromPath(host, pctx, "", installResult.BinaryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load terraform-provider plugin: %w", err)
	}
	defer func() {
		contract.IgnoreError(provider.Close())
	}()

	args := []string{tfProviderAddr}
	if tfProviderVersion != "" {
		args = append(args, tfProviderVersion)
	}

	paramResp, err := provider.Parameterize(ctx, plugin.ParameterizeRequest{
		Parameters: &plugin.ParameterizeArgs{
			Args: args,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to parameterize terraform-provider for %s: %w", tfProviderAddr, err)
	}

	parameterizedName := paramResp.Name
	mappingResp, err := provider.GetMapping(ctx, plugin.GetMappingRequest{
		Key:      "terraform",
		Provider: parameterizedName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get mapping for %s: %w", tfProviderAddr, err)
	}

	if mappingResp.Provider == "" || len(mappingResp.Data) == 0 {
		return nil, fmt.Errorf("terraform-provider returned empty mapping for %s", tfProviderAddr)
	}

	result := &GetMappingResult{
		Provider: mappingResp.Provider,
		Data:     mappingResp.Data,
	}
	providerInfo, err := UnmarshalMappingData(result)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal mapping for %s: %w", tfProviderAddr, err)
	}

	return providerInfo, nil
}
