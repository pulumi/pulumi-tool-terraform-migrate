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
	stackNameOverride string,
	projectNameOverride string,
) error {
	var tfState *tfjson.State
	var providerVersionMap map[string]string
	var err error

	if stateFilePath != "" {
		// Try to load the state file directly, detecting its format.
		tfState, providerVersionMap, err = loadStateFileDirectly(stateFilePath, tfDir)
		if err != nil {
			return err
		}
	} else {
		// No state file — use tofu to load from the project directory.
		loadOpts := tofu.LoadTerraformStateOptions{
			ProjectDir: tfDir,
		}
		tfState, err = tofu.LoadTerraformState(ctx, loadOpts)
		if err != nil {
			return err
		}

		providerVersions, pvErr := tofu.GetProviderVersions(ctx, tfDir)
		if pvErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to extract provider versions: %v\n", pvErr)
			providerVersions = tofu.TofuVersionOutput{}
		}
		providerVersionMap = providerVersions.ProviderSelections
	}

	// Resolve stack and project names from overrides or workspace fallback
	var stackName string
	if stackNameOverride != "" {
		stackName = stackNameOverride
	} else {
		stackName, err = getStackName(pulumiProgramDir)
		if err != nil {
			return fmt.Errorf("failed to get stack name: %w", err)
		}
	}

	var projectName string
	if projectNameOverride != "" {
		projectName = projectNameOverride
	} else {
		projectName, err = getProjectName(pulumiProgramDir)
		if err != nil {
			return fmt.Errorf("failed to get project name: %w", err)
		}
	}

	res, err := TranslateState(ctx, tfState, providerVersionMap, stackName, projectName)
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
}

func TranslateState(ctx context.Context, tfState *tfjson.State, providerVersions map[string]string, stackName, projectName string) (*TranslateStateResult, error) {
	pulumiProviders, err := GetPulumiProvidersForTerraformState(tfState, providerVersions)
	if err != nil {
		return nil, err
	}

	pulumiState, errorMessages, err := convertState(tfState, pulumiProviders)
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
	}, nil
}

type ErroredResource struct {
	ResourceName     string `json:"resource_name"`
	ResourceType     string `json:"resource_type"`
	ResourceProvider string `json:"resource_provider"`
	ErrorMessage     string `json:"error_message"`
}

func convertState(tfState *tfjson.State, pulumiProviders map[providermap.TerraformProviderName]*ProviderWithMetadata) (*PulumiState, []ErroredResource, error) {
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

	// Extract resource ID from converted properties, falling back to empty string.
	resourceID := ""
	if idProp, ok := props["id"]; ok && idProp.IsString() {
		resourceID = idProp.StringValue()
	}

	return PulumiResource{
		PulumiResourceID: PulumiResourceID{
			ID:   resourceID,
			Name: PulumiNameFromTerraformAddress(res.Address, res.Type),
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

// loadStateFileDirectly reads a state file, detects its format, and returns a tfjson.State
// without requiring the tofu binary. For raw .tfstate files, it parses them directly
// using the OpenTofu statefile library and converts to tfjson format. Provider versions
// are extracted from .terraform.lock.hcl in the TF directory if available.
func loadStateFileDirectly(stateFilePath, tfDir string) (*tfjson.State, map[string]string, error) {
	stateData, err := os.ReadFile(stateFilePath)
	if err != nil {
		return nil, nil, fmt.Errorf("reading state file: %w", err)
	}

	format, err := DetectStateFormatBytes(stateData)
	if err != nil {
		return nil, nil, fmt.Errorf("detecting state format: %w", err)
	}

	var tfState *tfjson.State

	switch format {
	case StateFormatTofuShowJSON:
		// Already in tfjson format — unmarshal directly.
		fmt.Fprintln(os.Stderr, "State file detected as tofu show -json format, parsing directly.")
		var st tfjson.State
		if err := json.Unmarshal(stateData, &st); err != nil {
			return nil, nil, fmt.Errorf("parsing tofu show JSON: %w", err)
		}
		tfState = &st

	case StateFormatRaw:
		// Raw .tfstate — parse with OpenTofu statefile library and convert.
		fmt.Fprintln(os.Stderr, "State file detected as raw .tfstate format, parsing directly (no tofu binary needed).")
		rawState, err := LoadRawStateBytes(stateData)
		if err != nil {
			return nil, nil, fmt.Errorf("parsing raw state: %w", err)
		}
		tfState = TfjsonFromRawState(rawState)
	}

	// Extract provider versions from .terraform.lock.hcl if available.
	providerVersions, err := tofu.GetProviderVersionsFromLockfile(tfDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to read lockfile for provider versions: %v\n", err)
		providerVersions = map[string]string{}
	}
	if len(providerVersions) > 0 {
		fmt.Fprintf(os.Stderr, "Extracted %d provider versions from .terraform.lock.hcl\n", len(providerVersions))
	}

	return tfState, providerVersions, nil
}

// PulumiNameFromTerraformAddress extracts a unique Pulumi resource name from a Terraform address.
// Terraform addresses have the format:
//   - Root module: <resource_type>.<name> e.g., "aws_s3_bucket.this"
//   - Submodule: module.<module_name>.<resource_type>.<name> e.g., "module.s3_bucket.aws_s3_bucket.this"
//   - Nested: module.<mod1>.module.<mod2>.<resource_type>.<name>
//
// We extract the module path and resource name (excluding the type) and join them with underscores.
// When the resource name is "this" (a Terraform convention for sole resources of a type in a module)
// and there is a module path to provide context, the "this" suffix is dropped.
func PulumiNameFromTerraformAddress(address, resourceType string) string {
	parts := strings.Split(address, ".")

	var moduleParts []string
	var resourceParts []string
	for i := 0; i < len(parts); i++ {
		if parts[i] == resourceType {
			resourceParts = append(resourceParts, parts[i+1:]...)
			break
		}
		if parts[i] == "module" && i+1 < len(parts) {
			moduleParts = append(moduleParts, parts[i+1])
			i++
		}
	}

	// Drop "this" suffix when module context provides a meaningful name.
	if len(moduleParts) > 0 && len(resourceParts) == 1 && resourceParts[0] == "this" {
		return strings.Join(moduleParts, "_")
	}

	nameParts := append(moduleParts, resourceParts...)
	return strings.Join(nameParts, "_")
}
