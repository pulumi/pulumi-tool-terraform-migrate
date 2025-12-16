package pkg

import (
	"testing"

	"github.com/hexops/autogold/v2"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
)

func TestInsertResourcesIntoDeployment(t *testing.T) {
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
