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
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping test in CI: TODO: set up pulumi credentials in CI")
	}
	ctx := context.Background()
	stackFolder := createPulumiStack(t)
	data, err := translateStateFromJson(ctx, "testdata/bucket_state.json", stackFolder)
	if err != nil {
		t.Fatalf("failed to convert Terraform state: %v", err)
	}

	autogold.ExpectFile(t, data.Export)
}

func TestConvertWithDependencies(t *testing.T) {
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping test in CI: TODO: set up pulumi credentials in CI")
	}
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
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping test in CI: TODO: set up pulumi credentials in CI")
	}
	ctx := context.Background()
	stackFolder := createPulumiStack(t)
	data, err := translateStateFromJson(ctx, "testdata/tofu_state.json", stackFolder)
	if err != nil {
		t.Fatalf("failed to convert Terraform state: %v", err)
	}

	autogold.ExpectFile(t, data.Export)
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
	return TranslateState(ctx, tfState, pulumiProgramDir)
}

func Test_convertState_simple(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/bucket_state.json",
	})
	require.NoError(t, err, "failed to load Terraform state")

	pulumiProviders, err := GetPulumiProvidersForTerraformState(tfState)
	require.NoError(t, err, "failed to get Pulumi providers")

	pulumiState, err := convertState(tfState, pulumiProviders)
	require.NoError(t, err, "failed to convert state")

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

	pulumiProviders, err := GetPulumiProvidersForTerraformState(tfState)
	require.NoError(t, err, "failed to get Pulumi providers")

	pulumiState, err := convertState(tfState, pulumiProviders)
	require.NoError(t, err, "failed to convert state")

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

func createPulumiStack(t *testing.T) string {
	dir, err := os.MkdirTemp("", "pulumi-stack-")
	require.NoError(t, err)
	t.Logf("Pulumi stack directory: %s", dir)

	_ = runCommand(t, dir, "pulumi", "new", "typescript", "--dir", dir, "--yes")
	_ = runCommand(t, dir, "pulumi", "up", "--yes")
	return dir
}
