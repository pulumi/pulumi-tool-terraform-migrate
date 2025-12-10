package pkg

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/valueshim"
	"github.com/pulumi/pulumi-terraform-migrate/pkg/bridge"
	"github.com/pulumi/pulumi-terraform-migrate/pkg/providermap"
	"github.com/pulumi/pulumi-terraform-migrate/pkg/tofu"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	_ "github.com/zclconf/go-cty/cty"
)

type StackExport struct {
	Deployment apitype.DeploymentV3 `json:"deployment"`
	Version    int                  `json:"version"`
}

// TranslateState translates a Terraform state file into a Pulumi state file.
//
// 1. Parse Terraform state files into the [tfjson.State] format with [tofu.LoadTerraformState]
// 2. Convert the [tfjson.State] format into the tf internal type system [cty.Value] via [tofu.StateToCtyValue] for each resource in the state.
//   - To do this we need the [cty.Type] corresponding to each resource in the state. We infer these from the Pulumi schema of the corresponding TF provider via [bridge.ImpliedType].
//
// 3. Produce a go `map[string]any` from the [cty.Value] via [bridge.ObjectFromCty]
// 4. Convert the `map[string]any` into the Pulumi internal type system [resource.PropertyMap] in [convertTFValueToPulumiValue]
// 5. Produce a pulumi state file from the [resource.PropertyMap] via [MakeDeployment]
func TranslateState(tofuStateFilePath string, pulumiProgramDir string) (*StackExport, error) {
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

	deployment, err := MakeDeployment(pulumiState, pulumiProgramDir)
	if err != nil {
		return nil, err
	}

	return &StackExport{
		Deployment: deployment,
		Version:    3,
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

	ctyResourceType, err := valueshim.ToCtyType(shimResource.SchemaType())
	if err != nil {
		return PulumiResource{}, fmt.Errorf("failed to convert resource type to CTY type: %w", err)
	}

	ctyValue, err := tofu.StateToCtyValue(res, ctyResourceType)
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
		// Parent:   stackUrn,
		// Provider: providerUrn,
	}, nil
}
