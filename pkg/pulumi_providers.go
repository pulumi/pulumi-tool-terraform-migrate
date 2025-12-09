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
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/contract"
)

func getTerraformProvidersForTerraformState(tfState *tfjson.State) ([]string, error) {
	tfProviders := map[string]struct{}{}

	err := tofu.VisitResources(tfState, func(resource *tfjson.StateResource) error {
		tfProviders[resource.ProviderName] = struct{}{}
		return nil
	}, &tofu.VisitOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to visit resources: %w", err)
	}

	providers := slices.Collect(maps.Keys(tfProviders))

	return providers, nil
}

func pulumiProvidersForTerraformProviders(terraformProviders []string) (map[string]*info.Provider, error) {
	pulumiProviders := make(map[string]*info.Provider)

	for _, providerName := range terraformProviders {
		pulumiProvider := providermap.RecommendPulumiProvider(providermap.TerraformProvider{
			Identifier: providerName,
		})

		// TODO: make this work for Any TF
		contract.Assertf(pulumiProvider.BridgedPulumiProvider != nil, "no bridged pulumi provider found for %s", providerName)

		result, err := bridgedproviders.InstallProvider(context.Background(), bridgedproviders.InstallProviderOptions{
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


func GetPulumiProvidersForTerraformState(tfState *tfjson.State) (map[string]*info.Provider, error) {
	terraformProviders, err := getTerraformProvidersForTerraformState(tfState)
	if err != nil {
		return nil, fmt.Errorf("failed to get terraform providers: %w", err)
	}
	return pulumiProvidersForTerraformProviders(terraformProviders)
}