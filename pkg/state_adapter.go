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
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/google/uuid"
	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/bridge"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/providermap"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tofu"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/zclconf/go-cty/cty"
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
	ctx context.Context,
	tfDir string,
	pulumiProgramDir string,
	outputFilePath string,
	requiredProvidersOutputFilePath string,
	strict bool,
) error {
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		ProjectDir: tfDir,
	})
	if err != nil {
		return err
	}

	providerVersions, err := tofu.GetProviderVersions(ctx, tfDir)
	if err != nil {
		// Log the error but don't fail - provider versions are optional
		fmt.Fprintf(os.Stderr, "Warning: failed to extract provider versions: %v\n", err)
		providerVersions = tofu.TofuVersionOutput{}
	}

	res, err := TranslateState(ctx, tfState, providerVersions.ProviderSelections, pulumiProgramDir)
	if err != nil {
		return err
	}
	if len(res.ErrorMessages) > 0 {
		for _, errorMessage := range res.ErrorMessages {
			fmt.Fprintf(os.Stderr, "failed to translate resource %s with type %s and provider %s: %v\n", errorMessage.ResourceName, errorMessage.ResourceType, errorMessage.ResourceProvider, errorMessage.ErrorMessage)
		}
		if strict {
			return fmt.Errorf("failed to translate state: %w", errors.New("failed to translate state for some resources"))
		}
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
	ErrorMessages     []ErroredResource
}

func TranslateState(ctx context.Context, tfState *tfjson.State, providerVersions map[string]string, pulumiProgramDir string) (*TranslateStateResult, error) {
	pulumiProviders, err := GetPulumiProvidersForTerraformState(tfState, providerVersions)
	if err != nil {
		return nil, err
	}

	pulumiState, errorMessages, err := convertState(tfState, pulumiProviders)
	if err != nil {
		return nil, fmt.Errorf("failed to convert state: %w", err)
	}

	deployment, err := GetDeployment(pulumiProgramDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	editedDeployment, err := InsertResourcesIntoDeployment(pulumiState, deployment.StackName, deployment.ProjectName, deployment.Deployment)
	if err != nil {
		return nil, fmt.Errorf("failed to insert resources into deployment: %w", err)
	}

	requiredProviders := slices.Collect(maps.Values(pulumiProviders))

	return &TranslateStateResult{
		Export: StackExport{
			Deployment: editedDeployment,
			Version:    3,
		},
		RequiredProviders: requiredProviders,
		ErrorMessages:     errorMessages,
	}, nil
}

type ErroredResource struct {
	ResourceName     string `json:"resource_name"`
	ResourceType     string `json:"resource_type"`
	ResourceProvider string `json:"resource_provider"`
	ErrorMessage     string `json:"error_message"`
}

func convertState(tfState *tfjson.State, pulumiProviders map[providermap.TerraformProviderName]*info.Provider) (*PulumiState, []ErroredResource, error) {
	pulumiState := &PulumiState{}

	// TODO[pulumi/pulumi-service#35512]: This assumes one Pulumi provider per Terraform provider.
	// This means that provider aliases are not supported.
	providerTable := map[providermap.TerraformProviderName]PulumiResourceID{}

	for tfProviderName, provider := range pulumiProviders {
		inputs, err := GetProviderInputs(provider.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get provider inputs: %w", err)
		}
		uuid := uuid.NewString()
		providerResource := PulumiResource{
			PulumiResourceID: PulumiResourceID{
				ID:   uuid,
				Type: "pulumi:providers:" + provider.Name,
				Name: "default_" + strings.ReplaceAll(provider.Version, ".", "_"),
			},
			Inputs:  inputs,
			Outputs: inputs,
			// No Provider link here as it is already a provider.
		}
		pulumiState.Providers = append(pulumiState.Providers, providerResource)
		providerTable[tfProviderName] = providerResource.PulumiResourceID
	}

	errorMessages := []ErroredResource{}

	err := tofu.VisitResources(tfState, func(resource *tfjson.StateResource) error {
		pulumiResource, err := convertResourceStateExceptProviderLink(resource, pulumiProviders)
		if err != nil {
			errorMessages = append(errorMessages, ErroredResource{
				ResourceName:     resource.Name,
				ResourceType:     resource.Type,
				ResourceProvider: resource.ProviderName,
				ErrorMessage:     err.Error(),
			})
			return nil
		}
		providerLink, ok := providerTable[providermap.TerraformProviderName(resource.ProviderName)]
		if !ok {
			return fmt.Errorf("failed resolving provider for Terraform resource at %q", resource.Address)
		}
		pulumiResource.Provider = &providerLink
		pulumiState.Resources = append(pulumiState.Resources, pulumiResource)
		return nil
	}, &tofu.VisitOptions{})
	if err != nil {
		return nil, errorMessages, fmt.Errorf("failed to visit resources: %w", err)
	}

	return pulumiState, errorMessages, nil
}

func convertResourceStateExceptProviderLink(
	res *tfjson.StateResource,
	pulumiProviders map[providermap.TerraformProviderName]*info.Provider,
) (PulumiResource, error) {
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

	var sensitivePaths []cty.Path
	if res.SensitiveValues != nil {
		sensitiveValues := map[string]interface{}{}
		err := json.Unmarshal(res.SensitiveValues, &sensitiveValues)
		if err != nil {
			return PulumiResource{}, fmt.Errorf("failed to unmarshal sensitive values: %w", err)
		}
		sensitivePaths = tofu.SensitiveObjToCtyPath(sensitiveValues)
	}

	pulumiTypeToken, err := bridge.PulumiTypeToken(res.Type, prov)
	if err != nil {
		return PulumiResource{}, fmt.Errorf("failed to get Pulumi type token: %w", err)
	}
	resourceInfo := prov.Resources[res.Type]
	props, err := convertTFValueToPulumiValue(ctyValue, shimResource, resourceInfo, sensitivePaths)
	if err != nil {
		return PulumiResource{}, fmt.Errorf("failed to convert value to Pulumi value: %w", err)
	}

	inputs, err := tfbridge.ExtractInputsFromOutputs(resource.PropertyMap{}, props, shimResource.Schema(), resourceInfo.Fields, false)
	if err != nil {
		return PulumiResource{}, fmt.Errorf("failed to extract inputs from outputs: %w", err)
	}

	return PulumiResource{
		PulumiResourceID: PulumiResourceID{
			ID:   props["id"].StringValue(),
			Name: res.Name,
			Type: string(pulumiTypeToken),
		},
		Inputs:  inputs,
		Outputs: props,
	}, nil
}
