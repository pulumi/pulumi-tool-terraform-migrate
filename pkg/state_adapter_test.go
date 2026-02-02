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
	"os"
	"testing"

	"github.com/hexops/autogold/v2"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tofu"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/stretchr/testify/require"
)

func TestConvertSimple(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stackFolder := createPulumiStack(t)
	data, err := translateStateFromJson(ctx, "testdata/bucket_state.json", stackFolder)
	if err != nil {
		t.Fatalf("failed to convert Terraform state: %v", err)
	}

	autogold.ExpectFile(t, data.Export)
}

func TestConvertWithDependencies(t *testing.T) {
	ctx := context.Background()
	stackFolder := createPulumiStack(t)
	res, err := translateStateFromJson(ctx, "testdata/bucket_state.json", stackFolder)
	if err != nil {
		t.Fatalf("failed to convert Terraform state: %v", err)
	}

	require.Equal(t, 1, len(res.RequiredProviders))
	require.Equal(t, "aws", res.RequiredProviders[0].Name)
	require.Equal(t, "7.12.0", res.RequiredProviders[0].Version)
}

func TestConvertInvolved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stackFolder := createPulumiStack(t)
	data, err := translateStateFromJson(ctx, "testdata/tofu_state.json", stackFolder)
	if err != nil {
		t.Fatalf("failed to convert Terraform state: %v", err)
	}

	autogold.ExpectFile(t, data.Export)
}

func TestConvertTwoModules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stackFolder := createPulumiStack(t)
	data, err := translateStateFromJson(ctx, "testdata/tofu_state_two_buckets.json", stackFolder)
	if err != nil {
		t.Fatalf("failed to convert Terraform state: %v", err)
	}

	bucketURNs := make(map[string]bool)
	for _, resource := range data.Export.Deployment.Resources {
		if resource.Type == "aws:s3/bucket:Bucket" {
			require.False(t, bucketURNs[string(resource.URN)], "URN %s is not unique", resource.URN)
			bucketURNs[string(resource.URN)] = true
		}
	}
	require.Equal(t, 2, len(bucketURNs), "expected 2 unique URNs for buckets")
}

func TestConvertNestedModules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stackFolder := createPulumiStack(t)
	data, err := translateStateFromJson(ctx, "testdata/tofu_state_nested_modules.json", stackFolder)
	if err != nil {
		t.Fatalf("failed to convert Terraform state: %v", err)
	}

	bucketURNs := make(map[string]bool)
	for _, resource := range data.Export.Deployment.Resources {
		if resource.Type == "aws:s3/bucket:Bucket" {
			require.False(t, bucketURNs[string(resource.URN)], "URN %s is not unique", resource.URN)
			bucketURNs[string(resource.URN)] = true
		}
	}
	require.Equal(t, 4, len(bucketURNs), "expected 4 unique URNs for buckets")
}

func TestConvertWithSensitiveValues(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stackFolder := createPulumiStack(t)
	data, err := translateStateFromJson(ctx, "testdata/tofu_random_sensitive_state.json", stackFolder)
	if err != nil {
		t.Fatalf("failed to convert Terraform state: %v", err)
	}

	password := data.Export.Deployment.Resources[2]
	require.Equal(t, tokens.Type("random:index/randomPassword:RandomPassword"), password.Type)
	_, ok := password.Outputs["result"].(*resource.Secret)
	require.True(t, ok)
}

func translateStateFromJson(ctx context.Context, tfStateJson string, pulumiProgramDir string) (*TranslateStateResult, error) {
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: tfStateJson,
	})
	if err != nil {
		return nil, err
	}
	// When loading from JSON, we don't have provider versions
	return TranslateState(ctx, tfState, nil, pulumiProgramDir)
}

func Test_convertState_simple(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/bucket_state.json",
	})
	require.NoError(t, err, "failed to load Terraform state")

	pulumiProviders, err := GetPulumiProvidersForTerraformState(tfState, nil)
	require.NoError(t, err, "failed to get Pulumi providers")

	pulumiState, errorMessages, err := convertState(tfState, pulumiProviders)
	require.NoError(t, err, "failed to convert state")
	require.Equal(t, 0, len(errorMessages), "expected no error messages")

	require.Equal(t, 1, len(pulumiState.Providers), "expected 1 provider")
	require.Equal(t, 1, len(pulumiState.Resources), "expected 1 resource")
	require.Equal(t, "pulumi:providers:aws", pulumiState.Providers[0].PulumiResourceID.Type)

	resource := pulumiState.Resources[0]
	require.NotNil(t, resource.Provider, "resource has no provider")
	provider, err := pulumiState.FindProvider(*resource.Provider)
	require.NoError(t, err, "failed to find provider for resource")
	require.Equal(t, "pulumi:providers:aws", provider.PulumiResourceID.Type)
}

func Test_convertState_multi_provider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/multi_provider_state/state.json",
	})
	require.NoError(t, err, "failed to load Terraform state")

	pulumiProviders, err := GetPulumiProvidersForTerraformState(tfState, nil)
	require.NoError(t, err, "failed to get Pulumi providers")

	pulumiState, errorMessages, err := convertState(tfState, pulumiProviders)
	require.NoError(t, err, "failed to convert state")
	require.Equal(t, 0, len(errorMessages), "expected no error messages")

	require.Equal(t, 2, len(pulumiState.Providers), "expected 2 providers")
	require.Equal(t, 2, len(pulumiState.Resources), "expected 2 resources")

	providerTypes := make(map[string]bool)
	for _, provider := range pulumiState.Providers {
		providerTypes[provider.PulumiResourceID.Type] = true
	}
	require.True(t, providerTypes["pulumi:providers:random"], "random provider should exist")
	require.True(t, providerTypes["pulumi:providers:tls"], "tls provider should exist")

	var randomResource *PulumiResource
	for i := range pulumiState.Resources {
		if pulumiState.Resources[i].PulumiResourceID.Type == "random:index/randomString:RandomString" {
			randomResource = &pulumiState.Resources[i]
			break
		}
	}
	require.NotNil(t, randomResource, "random_string resource not found")
	require.NotNil(t, randomResource.Provider, "random_string has no provider")
	randomProvider, err := pulumiState.FindProvider(*randomResource.Provider)
	require.NoError(t, err, "failed to find provider for random_string")
	require.Equal(t, "pulumi:providers:random", randomProvider.PulumiResourceID.Type,
		"random_string should be linked to random provider")

	var tlsResource *PulumiResource
	for i := range pulumiState.Resources {
		if pulumiState.Resources[i].PulumiResourceID.Type == "tls:index/privateKey:PrivateKey" {
			tlsResource = &pulumiState.Resources[i]
			break
		}
	}
	require.NotNil(t, tlsResource, "tls_private_key resource not found")
	require.NotNil(t, tlsResource.Provider, "tls_private_key has no provider")
	tlsProvider, err := pulumiState.FindProvider(*tlsResource.Provider)
	require.NoError(t, err, "failed to find provider for tls_private_key")
	require.Equal(t, "pulumi:providers:tls", tlsProvider.PulumiResourceID.Type,
		"tls_private_key should be linked to tls provider")
}

func Test_convertState_corrupted_state(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_corrupted_state.json",
	})
	require.NoError(t, err, "failed to load Terraform state")

	pulumiProviders, err := GetPulumiProvidersForTerraformState(tfState, nil)
	require.NoError(t, err, "failed to get Pulumi providers")

	_, errorMessages, err := convertState(tfState, pulumiProviders)
	require.NoError(t, err, "failed to convert state")
	require.Equal(t, 1, len(errorMessages), "expected 1 error message")
	require.Equal(t, "password", errorMessages[0].ResourceName)
	require.Equal(t, "random_password", errorMessages[0].ResourceType)
	require.Equal(t, "registry.opentofu.org/hashicorp/random", errorMessages[0].ResourceProvider)
	require.Contains(t, errorMessages[0].ErrorMessage, "unsupported attribute \"corrupted\"")
}

func Test_convertState_unknown_provider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/unknown_provider_state.json",
	})
	require.NoError(t, err, "failed to load Terraform state")

	pulumiProviders, err := GetPulumiProvidersForTerraformState(tfState, nil)
	require.NoError(t, err, "failed to get Pulumi providers")

	require.Len(t, pulumiProviders, 1, "should only have 1 provider (random)")

	pulumiState, errorMessages, err := convertState(tfState, pulumiProviders)
	require.NoError(t, err, "failed to convert state")

	require.Len(t, errorMessages, 1, "expected 1 error message for unknown_resource")
	require.Equal(t, "example", errorMessages[0].ResourceName)
	require.Equal(t, "unknown_resource", errorMessages[0].ResourceType)
	require.Equal(t, "registry.opentofu.org/hashicorp/unknown", errorMessages[0].ResourceProvider)
	require.Contains(t, errorMessages[0].ErrorMessage, "no bridged Pulumi provider found")

	require.Len(t, pulumiState.Providers, 1, "expected 1 provider")
	require.Len(t, pulumiState.Resources, 1, "expected 1 resource (unknown_resource should be skipped)")

	require.Equal(t, "random:index/randomString:RandomString", pulumiState.Resources[0].PulumiResourceID.Type)
}

func Test_pulumiNameFromTerraformAddress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		address      string
		resourceType string
		expected     string
	}{
		{
			name:         "root module resource",
			address:      "aws_s3_bucket.example",
			resourceType: "aws_s3_bucket",
			expected:     "example",
		},
		{
			name:         "single module resource",
			address:      "module.s3_bucket.aws_s3_bucket.this",
			resourceType: "aws_s3_bucket",
			expected:     "s3_bucket_this",
		},
		{
			name:         "nested module resource",
			address:      "module.outer.module.inner.aws_s3_bucket.mybucket",
			resourceType: "aws_s3_bucket",
			expected:     "outer_inner_mybucket",
		},
		{
			name:         "module with same name as resource",
			address:      "module.bucket.aws_s3_bucket.bucket",
			resourceType: "aws_s3_bucket",
			expected:     "bucket_bucket",
		},
		{
			name:         "module with module name",
			address:      "module.module.aws_s3_bucket.bucket",
			resourceType: "aws_s3_bucket",
			expected:     "module_bucket",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := pulumiNameFromTerraformAddress(tc.address, tc.resourceType)
			require.Equal(t, tc.expected, result)
		})
	}
}

func createPulumiStack(t *testing.T) string {
	dir, err := os.MkdirTemp("", "pulumi-stack-")
	require.NoError(t, err)
	t.Logf("Pulumi stack directory: %s", dir)

	_ = runCommand(t, dir, "pulumi", "new", "typescript", "--dir", dir, "--yes")
	_ = runCommand(t, dir, "pulumi", "up", "--yes")
	return dir
}
