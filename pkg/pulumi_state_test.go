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
	data, err := InsertResourcesIntoDeployment(&PulumiState{
		Providers: []PulumiResource{
			{
				ID:   "a339fe8e-e15d-4203-8719-c0ca5d3f414e",
				Type: "pulumi:providers:aws",
				Name: "default_7.12.0",
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
				ID:   "a339fe8e-e15d-4203-8719-c0ca5d3f414e",
				Type: "aws_s3_bucket",
				Name: "example",
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
				ID:   "a339fe8e-e15d-4203-8719-c0ca5d3f414e",
				Type: "aws_s3_bucket_object",
				Name: "example",
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

func skipIfCI(t *testing.T) {
	t.Helper()
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping test in CI: TODO: set up pulumi credentials in CI")
	}
}

func TestInsertResourcesIntoDeployment_ZeroResources(t *testing.T) {
	t.Parallel()
	_, err := InsertResourcesIntoDeployment(&PulumiState{
		Providers: []PulumiResource{
			{
				ID:   "a339fe8e-e15d-4203-8719-c0ca5d3f414e",
				Type: "pulumi:providers:aws",
				Name: "default_7.12.0",
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
				ID:   "a339fe8e-e15d-4203-8719-c0ca5d3f414e",
				Type: "pulumi:providers:aws",
				Name: "default_7.12.0",
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
	skipIfCI(t)
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
