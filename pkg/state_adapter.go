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
	"path/filepath"
	"slices"
	"strings"

	"github.com/google/uuid"
	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge"
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
	stateFilePath string,
	pulumiProgramDir string,
	outputFilePath string,
	requiredProvidersOutputFilePath string,
	strict bool,
	enableComponents bool,
	populateComponentInputs bool,
	typeOverrides map[string]string,
	sourceOverrides map[string]string,
	schemaOverrides map[string]string,
	stackNameOverride string,
	projectNameOverride string,
) error {
	loadOpts := tofu.LoadTerraformStateOptions{
		ProjectDir: tfDir,
	}
	if stateFilePath != "" {
		loadOpts = tofu.LoadTerraformStateOptions{
			StateFilePath: stateFilePath,
		}
	}
	tfState, err := tofu.LoadTerraformState(ctx, loadOpts)
	if err != nil {
		return err
	}

	providerVersions, err := tofu.GetProviderVersions(ctx, tfDir)
	if err != nil {
		// Log the error but don't fail - provider versions are optional
		fmt.Fprintf(os.Stderr, "Warning: failed to extract provider versions: %v\n", err)
		providerVersions = tofu.TofuVersionOutput{}
	}

	// Resolve stack and project names from overrides or workspace fallback
	var stackName string
	if stackNameOverride != "" {
		stackName = stackNameOverride
	} else {
		var err error
		stackName, err = getStackName(pulumiProgramDir)
		if err != nil {
			return fmt.Errorf("failed to get stack name: %w", err)
		}
	}

	var projectName string
	if projectNameOverride != "" {
		projectName = projectNameOverride
	} else {
		var err error
		projectName, err = getProjectName(pulumiProgramDir)
		if err != nil {
			return fmt.Errorf("failed to get project name: %w", err)
		}
	}

	res, err := TranslateState(ctx, tfState, providerVersions.ProviderSelections, stackName, projectName, enableComponents, populateComponentInputs, typeOverrides, sourceOverrides, schemaOverrides, tfDir)
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

	// Write component schema metadata sidecar file (always, when HCL sources are available)
	if res.ComponentMetadata != nil {
		metadataPath := filepath.Join(filepath.Dir(outputFilePath), "component-schemas.json")
		if err := WriteComponentSchemaMetadata(res.ComponentMetadata, metadataPath); err != nil {
			return fmt.Errorf("failed to write component schema metadata: %w", err)
		}
	}

	if requiredProvidersOutputFilePath != "" {
		requiredProviders := make([]RequiredProviderExport, 0, len(res.RequiredProviders))
		for _, provider := range res.RequiredProviders {
			name := provider.Name
			if provider.IsDynamic {
				name = formatDynamicProviderName(provider.TerraformAddress)
			}
			requiredProviders = append(requiredProviders, RequiredProviderExport{Name: name, Version: provider.Version})
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
	RequiredProviders []*ProviderWithMetadata
	ErrorMessages     []ErroredResource
	// ComponentMetadata is non-nil when HCL sources were available and parsed.
	ComponentMetadata *ComponentSchemaMetadata
}

func TranslateState(ctx context.Context, tfState *tfjson.State, providerVersions map[string]string, stackName, projectName string, enableComponents bool, populateComponentInputs bool, typeOverrides map[string]string, sourceOverrides map[string]string, schemaOverrides map[string]string, tfSourceDir string) (*TranslateStateResult, error) {
	pulumiProviders, err := GetPulumiProvidersForTerraformState(tfState, providerVersions)
	if err != nil {
		return nil, err
	}

	pulumiState, errorMessages, err := convertState(tfState, pulumiProviders, enableComponents, populateComponentInputs, typeOverrides, sourceOverrides, schemaOverrides, tfSourceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to convert state: %w", err)
	}

	editedDeployment, err := InsertResourcesIntoDeployment(pulumiState, stackName, projectName)
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
		ComponentMetadata: pulumiState.ComponentMetadata,
	}, nil
}

type ErroredResource struct {
	ResourceName     string `json:"resource_name"`
	ResourceType     string `json:"resource_type"`
	ResourceProvider string `json:"resource_provider"`
	ErrorMessage     string `json:"error_message"`
}

func convertState(tfState *tfjson.State, pulumiProviders map[providermap.TerraformProviderName]*ProviderWithMetadata, enableComponents bool, populateComponentInputs bool, typeOverrides map[string]string, sourceOverrides map[string]string, schemaOverrides map[string]string, tfSourceDir string) (*PulumiState, []ErroredResource, error) {
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

	// Build component tree from module hierarchy if enabled
	var componentTree []*componentNode
	if enableComponents {
		var resourceAddresses []string
		tofu.VisitResources(tfState, func(r *tfjson.StateResource) error {
			resourceAddresses = append(resourceAddresses, r.Address)
			return nil
		}, &tofu.VisitOptions{})

		if len(resourceAddresses) > 0 {
			var err error
			componentTree, err = buildComponentTree(resourceAddresses, typeOverrides)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to build component tree: %w", err)
			}
			pulumiState.Components = toComponents(componentTree, "")

			// Populate component inputs/outputs from HCL when source is available
			scopedAttrs := buildScopedResourceAttrMap(tfState)
			metadata, err := populateComponentsFromHCL(pulumiState.Components, componentTree, sourceOverrides, schemaOverrides, tfSourceDir, populateComponentInputs, scopedAttrs, tfState)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to populate component state from HCL: %w", err)
			}
			pulumiState.ComponentMetadata = metadata
		}
	}

	errorMessages := []ErroredResource{}

	err := tofu.VisitResources(tfState, func(resource *tfjson.StateResource) error {
		// Check if we have a Pulumi provider for this Terraform provider.
		// If not, skip the resource and add it to the error messages.
		providerLink, ok := providerTable[providermap.TerraformProviderName(resource.ProviderName)]
		if !ok {
			errorMessages = append(errorMessages, ErroredResource{
				ResourceName:     resource.Name,
				ResourceType:     resource.Type,
				ResourceProvider: resource.ProviderName,
				ErrorMessage:     fmt.Sprintf("no Pulumi provider available for Terraform provider %s (neither statically bridged nor dynamically bridged)", resource.ProviderName),
			})
			return nil
		}
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
		pulumiResource.Provider = &providerLink

		// When components are enabled, use short name and set parent
		if enableComponents {
			pulumiResource.Name = PulumiNameFromTerraformAddress(resource.Address, resource.Type, true)
			segments := parseModuleSegments(resource.Address)
			pulumiResource.Parent = componentParentForResource(componentTree, segments)
		}

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
	pulumiProviders map[providermap.TerraformProviderName]*ProviderWithMetadata,
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

	pulumiTypeToken, err := bridge.PulumiTypeToken(res.Type, prov.Provider)
	if err != nil {
		return PulumiResource{}, fmt.Errorf("failed to get Pulumi type token: %w", err)
	}
	resourceInfo := prov.Resources[res.Type]
	props, err := ConvertTFValueToPulumiValue(ctyValue, shimResource, resourceInfo, sensitivePaths)
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
			Name: PulumiNameFromTerraformAddress(res.Address, res.Type, false),
			Type: string(pulumiTypeToken),
		},
		Inputs:  inputs,
		Outputs: props,
	}, nil
}

// formatDynamicProviderName formats a Terraform provider address for use with the
// terraform-provider Pulumi package. For example:
// "registry.terraform.io/hashicorp/time" -> "terraform-provider hashicorp/time"
func formatDynamicProviderName(tfAddr string) string {
	// Split by "/" and take the last two parts (namespace/name)
	parts := strings.Split(tfAddr, "/")
	if len(parts) >= 2 {
		namespace := parts[len(parts)-2]
		name := parts[len(parts)-1]
		return fmt.Sprintf("terraform-provider %s/%s", namespace, name)
	}
	// Fallback: just use the whole address
	return "terraform-provider " + tfAddr
}

// PulumiNameFromTerraformAddress extracts a unique Pulumi resource name from a Terraform address.
// Terraform addresses have the format:
//   - Root module: <resource_type>.<name> e.g., "aws_s3_bucket.this"
//   - Submodule: module.<module_name>.<resource_type>.<name> e.g., "module.s3_bucket.aws_s3_bucket.this"
//   - Nested: module.<mod1>.module.<mod2>.<resource_type>.<name>
//
// When useShortName is true, only the resource name after the type is returned (module path is
// expressed via the parent component chain instead). When false, module path is baked into the name.
func PulumiNameFromTerraformAddress(address, resourceType string, useShortName bool) string {
	parts := strings.Split(address, ".")

	if useShortName {
		// Return only the resource name part (after the type)
		for i := 0; i < len(parts); i++ {
			if parts[i] == resourceType {
				return strings.Join(parts[i+1:], "_")
			}
		}
	}

	// Original behavior: include module path in name
	var nameParts []string
	for i := 0; i < len(parts); i++ {
		if parts[i] == resourceType {
			nameParts = append(nameParts, parts[i+1:]...)
			break
		}
		if parts[i] == "module" && i+1 < len(parts) {
			nameParts = append(nameParts, parts[i+1])
			i++
		}
	}

	return strings.Join(nameParts, "_")
}
