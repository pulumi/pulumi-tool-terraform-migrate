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

package providermap

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRecommendPulumiProvider(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                         string
		input                        TerraformProvider
		expectedBridgedProvider      string
		expectedVersion              string
		expectedUseTerraformProvider bool
	}{
		{
			name: "AWS Terraform registry - major version 5 with v prefix",
			input: TerraformProvider{
				Identifier: "registry.terraform.io/hashicorp/aws",
				Version:    "v5.0.0",
			},
			expectedBridgedProvider:      "aws",
			expectedVersion:              "v6.83.2",
			expectedUseTerraformProvider: false,
		},
		{
			name: "AWS Terraform registry - major version 5 without v prefix",
			input: TerraformProvider{
				Identifier: "registry.terraform.io/hashicorp/aws",
				Version:    "5.70.3",
			},
			expectedBridgedProvider:      "aws",
			expectedVersion:              "v6.83.2",
			expectedUseTerraformProvider: false,
		},
		{
			name: "AWS Terraform registry - major version 6",
			input: TerraformProvider{
				Identifier: "registry.terraform.io/hashicorp/aws",
				Version:    "v6.20.0",
			},
			expectedBridgedProvider:      "aws",
			expectedVersion:              "v7.16.0",
			expectedUseTerraformProvider: false,
		},
		{
			name: "AWS Terraform registry - precise match from versions.yaml",
			input: TerraformProvider{
				Identifier: "registry.terraform.io/hashicorp/aws",
				Version:    "v6.28.0",
			},
			expectedBridgedProvider:      "aws",
			expectedVersion:              "v7.16.0", // Precise match from versions.yaml
			expectedUseTerraformProvider: false,
		},
		{
			name: "AWS Terraform registry - precise match from versions.yaml (v6.27.0)",
			input: TerraformProvider{
				Identifier: "registry.terraform.io/hashicorp/aws",
				Version:    "6.27.0", // Without v prefix
			},
			expectedBridgedProvider:      "aws",
			expectedVersion:              "v7.15.0", // Precise match from versions.yaml
			expectedUseTerraformProvider: false,
		},
		{
			name: "AWS - no version specified (should use latest)",
			input: TerraformProvider{
				Identifier: "registry.terraform.io/hashicorp/aws",
				Version:    "",
			},
			expectedBridgedProvider:      "aws",
			expectedVersion:              "v7.16.0", // Latest version for highest major (6)
			expectedUseTerraformProvider: false,
		},
		{
			name: "AWS - invalid version (should use latest)",
			input: TerraformProvider{
				Identifier: "registry.terraform.io/hashicorp/aws",
				Version:    "invalid-version",
			},
			expectedBridgedProvider:      "aws",
			expectedVersion:              "v7.16.0", // Latest version for highest major (6)
			expectedUseTerraformProvider: false,
		},
		{
			name: "Azure Terraform registry - major version 3",
			input: TerraformProvider{
				Identifier: "registry.terraform.io/hashicorp/azurerm",
				Version:    "v3.0.0",
			},
			expectedBridgedProvider:      "azure",
			expectedVersion:              "v5.89.0",
			expectedUseTerraformProvider: false,
		},
		{
			name: "Azure Terraform registry - major version 4",
			input: TerraformProvider{
				Identifier: "registry.terraform.io/hashicorp/azurerm",
				Version:    "v4.15.0",
			},
			expectedBridgedProvider:      "azure",
			expectedVersion:              "v6.31.0",
			expectedUseTerraformProvider: false,
		},
		{
			name: "GCP OpenTofu registry - major version 6",
			input: TerraformProvider{
				Identifier: "registry.opentofu.org/hashicorp/google",
				Version:    "v6.5.0",
			},
			expectedBridgedProvider:      "gcp",
			expectedVersion:              "v8.41.1",
			expectedUseTerraformProvider: false,
		},
		{
			name: "GCP OpenTofu registry - major version 7",
			input: TerraformProvider{
				Identifier: "registry.opentofu.org/hashicorp/google",
				Version:    "7.0.0",
			},
			expectedBridgedProvider:      "gcp",
			expectedVersion:              "v9.10.0",
			expectedUseTerraformProvider: false,
		},
		{
			name: "Unknown provider",
			input: TerraformProvider{
				Identifier: "registry.terraform.io/somevendor/customprovider",
				Version:    "v1.0.0",
			},
			expectedBridgedProvider:      "",
			expectedVersion:              "",
			expectedUseTerraformProvider: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RecommendPulumiProvider(tt.input)

			if tt.expectedUseTerraformProvider {
				if !result.UseTerraformProviderPackage {
					t.Errorf("Expected UseTerraformProviderPackage to be true, got false")
				}
				if result.BridgedPulumiProvider != nil {
					t.Errorf("Expected BridgedPulumiProvider to be nil, got %v", result.BridgedPulumiProvider)
				}
			} else {
				if result.UseTerraformProviderPackage {
					t.Errorf("Expected UseTerraformProviderPackage to be false, got true")
				}
				if result.BridgedPulumiProvider == nil {
					t.Errorf("Expected BridgedPulumiProvider to be non-nil, got nil")
				} else {
					if result.BridgedPulumiProvider.Identifier != tt.expectedBridgedProvider {
						t.Errorf("Expected BridgedPulumiProvider.Identifier to be %q, got %q",
							tt.expectedBridgedProvider, result.BridgedPulumiProvider.Identifier)
					}
					if result.BridgedPulumiProvider.Version != tt.expectedVersion {
						t.Errorf("Expected BridgedPulumiProvider.Version to be %q, got %q",
							tt.expectedVersion, result.BridgedPulumiProvider.Version)
					}
				}
			}
		})
	}
}

func TestProviderMappingUsesProvidersThatExist(t *testing.T) {
	t.Parallel()
	for k := range providerMapping {
		parts := strings.Split(string(k), "/")
		ok, err := checkProviderExists(context.Background(), parts[0], parts[1], parts[2])
		assert.NoError(t, err)
		assert.Truef(t, ok, string(k))
	}
}

// CheckProviderExists checks if a provider exists in the given registry
// Example: CheckProviderExists(ctx, "registry.opentofu.org", "hashicorp", "consul")
func checkProviderExists(ctx context.Context, registryHost, namespace, providerType string) (bool, error) {
	// Registry API endpoint format varies by registry:
	// - Terraform: https://{host}/v1/providers/{namespace}/{type}
	// - OpenTofu: https://{host}/v1/providers/{namespace}/{type}/versions
	var url string
	if registryHost == "registry.opentofu.org" {
		url = fmt.Sprintf("https://%s/v1/providers/%s/%s/versions", registryHost, namespace, providerType)
	} else {
		url = fmt.Sprintf("https://%s/v1/providers/%s/%s", registryHost, namespace, providerType)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("creating request: %w", err)
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
}
