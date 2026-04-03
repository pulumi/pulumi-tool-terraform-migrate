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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hexops/autogold/v2"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/stretchr/testify/require"
)

func TestInsertResourcesIntoDeployment(t *testing.T) {
	t.Parallel()

	awsProviderID := PulumiResourceID{
		ID:   "a339fe8e-e15d-4203-8719-c0ca5d3f414e",
		Type: "pulumi:providers:aws",
		Name: "default_7.12.0",
	}

	data, err := InsertResourcesIntoDeployment(&PulumiState{
		Providers: []PulumiResource{
			{
				PulumiResourceID: awsProviderID,
				Inputs: resource.PropertyMap{
					"region":                    resource.NewProperty("us-east-1"),
					"skipCredentialsValidation": resource.NewProperty(false),
					"skipRegionValidation":      resource.NewProperty(true),
					"version":                   resource.NewProperty("7.12.0"),
				},
				Outputs: resource.PropertyMap{
					"region":                    resource.NewProperty("us-east-1"),
					"skipCredentialsValidation": resource.NewProperty(false),
					"skipRegionValidation":      resource.NewProperty(true),
					"version":                   resource.NewProperty("7.12.0"),
				},
			},
		},
		Resources: []PulumiResource{
			{
				PulumiResourceID: PulumiResourceID{
					ID:   "a339fe8e-e15d-4203-8719-c0ca5d3f414e",
					Type: "aws_s3_bucket",
					Name: "example",
				},
				Provider: &awsProviderID,
				Inputs: resource.PropertyMap{
					"bucket":  resource.NewProperty("example"),
					"key":     resource.NewProperty("example"),
					"content": resource.NewProperty("example"),
				},
				Outputs: resource.PropertyMap{
					"bucket":  resource.NewProperty("example"),
					"key":     resource.NewProperty("example"),
					"content": resource.NewProperty("example"),
				},
			},
			{
				PulumiResourceID: PulumiResourceID{
					ID:   "a339fe8e-e15d-4203-8719-c0ca5d3f414e",
					Type: "aws_s3_bucket_object",
					Name: "example",
				},
				Provider: &awsProviderID,
				Inputs: resource.PropertyMap{
					"bucket":  resource.NewProperty("example"),
					"key":     resource.NewProperty("example"),
					"content": resource.NewProperty("example"),
				},
				Outputs: resource.PropertyMap{
					"bucket":  resource.NewProperty("example"),
					"key":     resource.NewProperty("example"),
					"content": resource.NewProperty("example"),
				},
			},
		},
	}, "dev", "example")
	if err != nil {
		t.Fatalf("failed to make deployment: %v", err)
	}

	// Sanitize the timestamps to make the test deterministic
	for i := range data.Resources {
		data.Resources[i].Created = nil
		data.Resources[i].Modified = nil
	}

	autogold.ExpectFile(t, data)
}

func TestInsertResourcesIntoDeployment_multi_provider(t *testing.T) {
	t.Parallel()

	randomProviderID := PulumiResourceID{
		ID:   "random-provider-id",
		Type: "pulumi:providers:random",
		Name: "default_4.18.1",
	}

	tlsProviderID := PulumiResourceID{
		ID:   "tls-provider-id",
		Type: "pulumi:providers:tls",
		Name: "default_5.2.3",
	}

	data, err := InsertResourcesIntoDeployment(&PulumiState{
		Providers: []PulumiResource{
			{
				PulumiResourceID: randomProviderID,
				Inputs: resource.PropertyMap{
					"version": resource.NewProperty("4.18.1"),
				},
				Outputs: resource.PropertyMap{
					"version": resource.NewProperty("4.18.1"),
				},
			},
			{
				PulumiResourceID: tlsProviderID,
				Inputs: resource.PropertyMap{
					"version": resource.NewProperty("5.2.3"),
				},
				Outputs: resource.PropertyMap{
					"version": resource.NewProperty("5.2.3"),
				},
			},
		},
		Resources: []PulumiResource{
			{
				PulumiResourceID: PulumiResourceID{
					ID:   "random-string-id",
					Type: "random:index/randomString:RandomString",
					Name: "example",
				},
				Provider: &randomProviderID,
				Inputs: resource.PropertyMap{
					"length": resource.NewProperty(float64(16)),
				},
				Outputs: resource.PropertyMap{
					"length": resource.NewProperty(float64(16)),
					"result": resource.NewProperty("test-random-value"),
				},
			},
			{
				PulumiResourceID: PulumiResourceID{
					ID:   "tls-key-id",
					Type: "tls:index/privateKey:PrivateKey",
					Name: "example",
				},
				Provider: &tlsProviderID,
				Inputs: resource.PropertyMap{
					"algorithm": resource.NewProperty("RSA"),
					"rsaBits":   resource.NewProperty(float64(4096)),
				},
				Outputs: resource.PropertyMap{
					"algorithm": resource.NewProperty("RSA"),
					"rsaBits":   resource.NewProperty(float64(4096)),
				},
			},
		},
	}, "dev", "example")
	require.NoError(t, err, "failed to make deployment")

	require.Equal(t, 5, len(data.Resources), "expected 5 resources (1 stack, 2 providers, 2 resources)")

	var randomProvider, tlsProvider *apitype.ResourceV3
	for i := range data.Resources {
		if data.Resources[i].Type == "pulumi:providers:random" {
			randomProvider = &data.Resources[i]
		}
		if data.Resources[i].Type == "pulumi:providers:tls" {
			tlsProvider = &data.Resources[i]
		}
	}
	require.NotNil(t, randomProvider, "random provider should be in deployment")
	require.NotNil(t, tlsProvider, "tls provider should be in deployment")

	var randomStringResource, tlsKeyResource *apitype.ResourceV3
	for i := range data.Resources {
		if data.Resources[i].Type == "random:index/randomString:RandomString" {
			randomStringResource = &data.Resources[i]
		}
		if data.Resources[i].Type == "tls:index/privateKey:PrivateKey" {
			tlsKeyResource = &data.Resources[i]
		}
	}
	require.NotNil(t, randomStringResource, "random_string resource should be in deployment")
	require.NotNil(t, tlsKeyResource, "tls_private_key resource should be in deployment")

	// The Provider field is a string in the format: "urn::provider-id"
	require.Contains(t, string(randomStringResource.Provider), string(randomProvider.URN),
		"random_string should be linked to random provider")
	require.Contains(t, string(tlsKeyResource.Provider), string(tlsProvider.URN),
		"tls_private_key should be linked to tls provider")
}

func TestInsertResourcesIntoDeployment_EmptyStackName(t *testing.T) {
	t.Parallel()
	_, err := InsertResourcesIntoDeployment(&PulumiState{}, "", "project")
	require.Error(t, err)
	require.Contains(t, err.Error(), "stackName")
}

func TestInsertResourcesIntoDeployment_EmptyProjectName(t *testing.T) {
	t.Parallel()
	_, err := InsertResourcesIntoDeployment(&PulumiState{}, "dev", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "projectName")
}

func TestInsertResourcesIntoDeployment_WithComponents(t *testing.T) {
	stackName := "dev"
	projectName := "testproject"

	providerID := PulumiResourceID{ID: "provider-uuid", Name: "default_6_0_0", Type: "pulumi:providers:aws"}
	state := &PulumiState{
		Providers: []PulumiResource{
			{PulumiResourceID: providerID, Inputs: resource.PropertyMap{}, Outputs: resource.PropertyMap{}},
		},
		Components: []PulumiResource{
			{
				PulumiResourceID: PulumiResourceID{Name: "vpc", Type: "terraform:module/vpc:Vpc"},
				Parent:           "",
			},
		},
		Resources: []PulumiResource{
			{
				PulumiResourceID: PulumiResourceID{ID: "subnet-123", Name: "this", Type: "aws:ec2/subnet:Subnet"},
				Provider:         &providerID,
				Parent:           "terraform:module/vpc:Vpc",
				Inputs:           resource.PropertyMap{},
				Outputs:          resource.PropertyMap{},
			},
		},
	}

	result, err := InsertResourcesIntoDeployment(state, stackName, projectName)
	require.NoError(t, err)

	// Stack + provider + component + resource = 4
	require.Len(t, result.Resources, 4)

	// Verify ordering: Stack, provider, component, resource
	require.Equal(t, tokens.Type("pulumi:pulumi:Stack"), result.Resources[0].Type)
	require.True(t, result.Resources[1].Custom)  // provider
	require.False(t, result.Resources[2].Custom) // component
	require.True(t, result.Resources[3].Custom)  // resource

	// Verify component resource
	component := result.Resources[2]
	require.False(t, component.Custom)
	require.Equal(t, tokens.Type("terraform:module/vpc:Vpc"), component.Type)
	require.Empty(t, component.ID)
	require.Empty(t, component.Provider)

	// Verify resource is parented to component
	res := result.Resources[3]
	require.Contains(t, string(res.Parent), "terraform:module/vpc:Vpc")
}

func TestInsertResourcesIntoDeployment_NestedComponents(t *testing.T) {
	stackName := "dev"
	projectName := "testproject"

	providerID := PulumiResourceID{ID: "pid", Name: "default_1_0_0", Type: "pulumi:providers:aws"}
	state := &PulumiState{
		Providers: []PulumiResource{
			{PulumiResourceID: providerID, Inputs: resource.PropertyMap{}, Outputs: resource.PropertyMap{}},
		},
		Components: []PulumiResource{
			{
				PulumiResourceID: PulumiResourceID{Name: "vpc", Type: "terraform:module/vpc:Vpc"},
				Parent:           "",
			},
			{
				PulumiResourceID: PulumiResourceID{Name: "subnets", Type: "terraform:module/subnets:Subnets"},
				Parent:           "terraform:module/vpc:Vpc",
			},
		},
		Resources: []PulumiResource{
			{
				PulumiResourceID: PulumiResourceID{ID: "subnet-1", Name: "this", Type: "aws:ec2/subnet:Subnet"},
				Provider:         &providerID,
				Parent:           "terraform:module/vpc:Vpc$terraform:module/subnets:Subnets",
				Inputs:           resource.PropertyMap{},
				Outputs:          resource.PropertyMap{},
			},
		},
	}

	result, err := InsertResourcesIntoDeployment(state, stackName, projectName)
	require.NoError(t, err)
	require.Len(t, result.Resources, 5) // Stack + provider + 2 components + resource

	// subnets component should be parented to vpc component
	subnets := result.Resources[3]
	require.False(t, subnets.Custom)
	require.Contains(t, string(subnets.Parent), "terraform:module/vpc:Vpc")

	// resource should be parented to subnets component
	res := result.Resources[4]
	require.Contains(t, string(res.Parent), "terraform:module/subnets:Subnets")

	// URN should encode parent type chain with $ delimiter
	require.True(t, strings.Contains(string(res.URN), "terraform:module/vpc:Vpc$terraform:module/subnets:Subnets$aws:ec2/subnet:Subnet"))
}

func TestInsertResourcesIntoDeployment_NoComponents_BackwardCompat(t *testing.T) {
	stackName := "dev"
	projectName := "testproject"

	providerID := PulumiResourceID{ID: "pid", Name: "default_1_0_0", Type: "pulumi:providers:random"}
	state := &PulumiState{
		Providers: []PulumiResource{
			{PulumiResourceID: providerID, Inputs: resource.PropertyMap{}, Outputs: resource.PropertyMap{}},
		},
		Components: nil,
		Resources: []PulumiResource{
			{
				PulumiResourceID: PulumiResourceID{ID: "abc", Name: "test", Type: "random:index/randomPet:RandomPet"},
				Provider:         &providerID,
				Inputs:           resource.PropertyMap{},
				Outputs:          resource.PropertyMap{},
			},
		},
	}

	result, err := InsertResourcesIntoDeployment(state, stackName, projectName)
	require.NoError(t, err)
	require.Len(t, result.Resources, 3) // Stack + provider + resource
	// Resource parent should be Stack
	require.Contains(t, string(result.Resources[2].Parent), "pulumi:pulumi:Stack")
}

func TestGetProjectName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "Pulumi.yaml"), []byte("name: my-project\nruntime: go\n"), 0644)
	require.NoError(t, err)

	name, err := getProjectName(dir)
	require.NoError(t, err)
	require.Equal(t, "my-project", name)
}

func TestGetProjectName_Missing(t *testing.T) {
	t.Parallel()
	_, err := getProjectName(t.TempDir())
	require.Error(t, err)
	require.Contains(t, err.Error(), "Pulumi.yaml")
}

func TestGetProjectName_EmptyName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "Pulumi.yaml"), []byte("runtime: go\n"), 0644)
	require.NoError(t, err)

	_, err = getProjectName(dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty")
}
