package pkg

import (
	"context"
	"fmt"
	"maps"
	"slices"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	"github.com/pulumi/pulumi-terraform-migrate/pkg/bridgedproviders"
	"github.com/pulumi/pulumi-terraform-migrate/pkg/providermap"
	"github.com/pulumi/pulumi-terraform-migrate/pkg/tofu"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/contract"
)

func getTerraformProvidersForTerraformState(tfState *tfjson.State) ([]providermap.TerraformProviderName, error) {
	tfProviders := map[providermap.TerraformProviderName]struct{}{}

	err := tofu.VisitResources(tfState, func(resource *tfjson.StateResource) error {
		tfProviders[providermap.TerraformProviderName(resource.ProviderName)] = struct{}{}
		return nil
	}, &tofu.VisitOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to visit resources: %w", err)
	}

	providers := slices.Collect(maps.Keys(tfProviders))

	return providers, nil
}

func pulumiProvidersForTerraformProviders(terraformProviders []providermap.TerraformProviderName) (map[providermap.TerraformProviderName]*info.Provider, error) {
	pulumiProviders := make(map[providermap.TerraformProviderName]*info.Provider)

	for _, providerName := range terraformProviders {
		pulumiProvider := providermap.RecommendPulumiProvider(providermap.TerraformProvider{
			Identifier: providermap.TerraformProviderName(providerName),
		})

		// TODO[pulumi/pulumi-service#35437]: make this work for Any TF
		contract.Assertf(pulumiProvider.BridgedPulumiProvider != nil, "no bridged pulumi provider found for %s", providerName)

		result, err := bridgedproviders.EnsureProviderInstalled(context.Background(), bridgedproviders.InstallProviderOptions{
			Name:    pulumiProvider.BridgedPulumiProvider.Identifier,
			Version: pulumiProvider.BridgedPulumiProvider.Version,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to install provider %s: %w", providerName, err)
		}

		mapping, err := bridgedproviders.GetMappingFromBinary(context.Background(), result.BinaryPath, bridgedproviders.GetMappingOptions{
			Key:      "terraform",
			Provider: pulumiProvider.BridgedPulumiProvider.Identifier,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get mapping for provider %s: %w", providerName, err)
		}

		providerInfo, err := bridgedproviders.UnmarshalMappingData(mapping)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal mapping for provider %s: %w", providerName, err)
		}

		pulumiProviders[providerName] = providerInfo
	}
	return pulumiProviders, nil
}


func GetPulumiProvidersForTerraformState(tfState *tfjson.State) (map[providermap.TerraformProviderName]*info.Provider, error) {
	// TODO[pulumi/pulumi-service#35512: This assumes one Pulumi provider per Terraform provider. This means that provider aliases are not supported.
	terraformProviders, err := getTerraformProvidersForTerraformState(tfState)
	if err != nil {
		return nil, fmt.Errorf("failed to get terraform providers: %w", err)
	}
	return pulumiProvidersForTerraformProviders(terraformProviders)
}

func GetProviderInputs(providerName string) (resource.PropertyMap, error) {
	// TODO: call the CheckConfig GRPC method
	switch providerName {
	case "aws":
		return resource.PropertyMap{
			"region":                    resource.NewProperty("us-east-1"),
			"skipCredentialsValidation": resource.NewProperty(false),
			"skipRegionValidation":      resource.NewProperty(true),
			"version":                   resource.NewProperty("7.12.0"),
		}, nil
	case "archive":
		return resource.PropertyMap{
			"version": resource.NewProperty("0.3.5"),
		}, nil
	case "random":
		return resource.PropertyMap{
			"version": resource.NewProperty("4.18.1"),
		}, nil
	}
	return nil, fmt.Errorf("unsupported provider: %s", providerName)
}
