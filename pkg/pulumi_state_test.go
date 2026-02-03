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
	"os/exec"
	"testing"

	"github.com/hexops/autogold/v2"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
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
	}, "dev", "example", apitype.DeploymentV3{
		Resources: []apitype.ResourceV3{
			{
				URN:  "urn:pulumi:dev::example::pulumi:pulumi:Stack::example-dev",
				Type: "pulumi:pulumi:Stack",
				ID:   "a339fe8e-e15d-4203-8719-c0ca5d3f414e",
			},
		},
	})
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
	}, "dev", "example", apitype.DeploymentV3{
		Resources: []apitype.ResourceV3{
			{
				URN:  "urn:pulumi:dev::example::pulumi:pulumi:Stack::example-dev",
				Type: "pulumi:pulumi:Stack",
				ID:   "stack-id",
			},
		},
	})
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

func runCommand(t *testing.T, dir string, command string, args ...string) string {
	t.Helper()
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("failed to run command %s %v, error: %v, output: %s, stdout: %s", command, args, err, string(exitErr.Stderr), output)
		}
		t.Fatalf("failed to run command %s %v, error: %v", command, args, err)
	}
	return string(output)
}

func TestInsertResourcesIntoDeployment_ZeroResources(t *testing.T) {
	t.Parallel()
	_, err := InsertResourcesIntoDeployment(&PulumiState{
		Providers: []PulumiResource{
			{
				PulumiResourceID: PulumiResourceID{
					ID:   "a339fe8e-e15d-4203-8719-c0ca5d3f414e",
					Type: "pulumi:providers:aws",
					Name: "default_7.12.0",
				},
			},
		},
		Resources: []PulumiResource{},
	}, "dev", "example", apitype.DeploymentV3{
		Resources: []apitype.ResourceV3{},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "No Stack resource found")
}

func TestInsertResourcesIntoDeployment_MultipleResources(t *testing.T) {
	t.Parallel()
	_, err := InsertResourcesIntoDeployment(&PulumiState{
		Providers: []PulumiResource{
			{
				PulumiResourceID: PulumiResourceID{
					ID:   "a339fe8e-e15d-4203-8719-c0ca5d3f414e",
					Type: "pulumi:providers:aws",
					Name: "default_7.12.0",
				},
			},
		},
		Resources: []PulumiResource{},
	}, "dev", "example", apitype.DeploymentV3{
		Resources: []apitype.ResourceV3{
			{
				URN:  "urn:pulumi:dev::example::pulumi:pulumi:Stack::example-dev",
				Type: "pulumi:pulumi:Stack",
				ID:   "a339fe8e-e15d-4203-8719-c0ca5d3f414e",
			},
			{
				URN:  "urn:pulumi:dev::example::aws:s3/bucket:Bucket::my-bucket",
				Type: "aws:s3/bucket:Bucket",
				ID:   "b339fe8e-e15d-4203-8719-c0ca5d3f414f",
			},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Found 2 resources")
	require.Contains(t, err.Error(), "expected 1")
}

func TestGetDeployment(t *testing.T) {
	testDir, err := os.MkdirTemp("", "test-deployment-*")
	require.NoError(t, err)
	defer os.RemoveAll(testDir)

	_ = runCommand(t, testDir, "pulumi", "new", "typescript", "--yes")
	_ = runCommand(t, testDir, "pulumi", "stack", "select", "dev")
	_ = runCommand(t, testDir, "pulumi", "up", "--yes")

	deployment, err := GetDeployment(testDir)
	require.NoError(t, err)
	require.Equal(t, 1, len(deployment.Deployment.Resources))
}
