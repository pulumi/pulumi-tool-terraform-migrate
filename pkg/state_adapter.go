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
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/google/uuid"
	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	"github.com/pulumi/pulumi-terraform-migrate/pkg/bridge"
	"github.com/pulumi/pulumi-terraform-migrate/pkg/providermap"
	"github.com/pulumi/pulumi-terraform-migrate/pkg/tofu"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
)

type StackExport struct {
	Deployment apitype.DeploymentV3 `json:"deployment"`
	Version    int                  `json:"version"`
}

type RequiredProviderExport struct {
	// The name of the Pulumi provider, such as "aws" or "azure" or "gcp".
	Name string `json:"name"`
	// The version of the Pulumi provider, such as "7.12.0" or "6.30.0".
	Version string `json:"version"`
}

func TranslateAndWriteState(
	tofuStateFilePath string, pulumiProgramDir string, outputFilePath string, requiredProvidersOutputFilePath string,
) error {
	res, err := TranslateState(tofuStateFilePath, pulumiProgramDir)
	if err != nil {
		return err
	}
	bytes, err := json.Marshal(res.Export)
	if err != nil {
		return fmt.Errorf("failed to marshal stack export: %w", err)
	}
	err = os.WriteFile(outputFilePath, bytes, 0o600)
	if err != nil {
		return fmt.Errorf("failed to write stack export: %w", err)
	}

	if requiredProvidersOutputFilePath != "" {
		requiredProviders := make([]RequiredProviderExport, 0, len(res.RequiredProviders))
		for _, provider := range res.RequiredProviders {
			requiredProviders = append(requiredProviders, RequiredProviderExport{Name: provider.Name, Version: provider.Version})
		}
		bytes, err := json.Marshal(requiredProviders)
		if err != nil {
			return fmt.Errorf("failed to marshal required providers: %w", err)
		}
		err = os.WriteFile(requiredProvidersOutputFilePath, bytes, 0o600)
		if err != nil {
			return fmt.Errorf("failed to write required providers: %w", err)
		}
	}
	return nil
}

type TranslateStateResult struct {
	Export            StackExport
	RequiredProviders []*info.Provider
}

func TranslateState(tofuStateFilePath string, pulumiProgramDir string) (*TranslateStateResult, error) {
	tfState, err := tofu.LoadTerraformState(tofuStateFilePath)
	if err != nil {
		return nil, err
	}

	pulumiProviders, err := GetPulumiProvidersForTerraformState(tfState)
	if err != nil {
		return nil, err
	}

	pulumiState, err := convertState(tfState, pulumiProviders)
	if err != nil {
		return nil, err
	}

	deployment, err := GetDeployment(pulumiProgramDir)
	if err != nil {
		return nil, err
	}

	editedDeployment, err := InsertResourcesIntoDeployment(pulumiState, deployment.StackName, deployment.ProjectName, deployment.Deployment)
	if err != nil {
		return nil, err
	}

	requiredProviders := slices.Collect(maps.Values(pulumiProviders))

	return &TranslateStateResult{
		Export: StackExport{
			Deployment: editedDeployment,
			Version:    3,
		},
		RequiredProviders: requiredProviders,
	}, nil
}

func convertState(tfState *tfjson.State, pulumiProviders map[providermap.TerraformProviderName]*info.Provider) (*PulumiState, error) {
	pulumiState := &PulumiState{}

	for _, provider := range pulumiProviders {
		inputs, err := GetProviderInputs(provider.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get provider inputs: %w", err)
		}
		uuid := uuid.NewString()
		pulumiState.Providers = append(pulumiState.Providers, PulumiResource{
			ID:      uuid,
			Type:    "pulumi:providers:" + provider.Name,
			Name:    "default_" + strings.ReplaceAll(provider.Version, ".", "_"),
			Inputs:  inputs,
			Outputs: inputs,
		})
	}

	err := tofu.VisitResources(tfState, func(resource *tfjson.StateResource) error {
		pulumiResource, err := convertResourceState(resource, pulumiProviders)
		if err != nil {
			return fmt.Errorf("failed to convert resource state for %s with ID %s: %w", resource.Type, resource.Address, err)
		}
		pulumiState.Resources = append(pulumiState.Resources, pulumiResource)
		return nil
	}, &tofu.VisitOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to visit resources: %w", err)
	}

	return pulumiState, nil
}

func convertResourceState(res *tfjson.StateResource, pulumiProviders map[providermap.TerraformProviderName]*info.Provider) (PulumiResource, error) {
	prov, ok := pulumiProviders[providermap.TerraformProviderName(res.ProviderName)]
	if !ok {
		return PulumiResource{}, fmt.Errorf("no Pulumi provider found for Terraform provider: %s", res.ProviderName)
	}
	shimResource := prov.P.ResourcesMap().Get(res.Type)
	if shimResource == nil {
		return PulumiResource{}, fmt.Errorf("no resource type found for Terraform resource: %s", res.Type)
	}

	ctyType := bridge.ImpliedType(shimResource.Schema(), true)
	ctyValue, err := tofu.StateToCtyValue(res, ctyType)
	if err != nil {
		return PulumiResource{}, fmt.Errorf("failed to convert resource to CTY value: %w", err)
	}

	pulumiTypeToken, err := bridge.PulumiTypeToken(res.Type, prov)
	if err != nil {
		return PulumiResource{}, fmt.Errorf("failed to get Pulumi type token: %w", err)
	}
	resourceInfo := prov.Resources[res.Type]
	props, err := convertTFValueToPulumiValue(ctyValue, shimResource, resourceInfo)
	if err != nil {
		return PulumiResource{}, fmt.Errorf("failed to convert value to Pulumi value: %w", err)
	}

	inputs, err := tfbridge.ExtractInputsFromOutputs(resource.PropertyMap{}, props, shimResource.Schema(), resourceInfo.Fields, false)
	if err != nil {
		return PulumiResource{}, fmt.Errorf("failed to extract inputs from outputs: %w", err)
	}

	return PulumiResource{
		ID:      props["id"].StringValue(),
		Type:    string(pulumiTypeToken),
		Inputs:  inputs,
		Outputs: props,
		Name:    res.Name,
	}, nil
}
