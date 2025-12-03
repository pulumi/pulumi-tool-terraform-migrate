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

// Package upsert provides functionality to inject resources into Pulumi state
// without actually creating them. It uses a mock provider that responds to Create()
// calls with predefined outputs, allowing resources to be added to state using
// `pulumi up --target`.
package upsert

import (
	"context"
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
)

// ResourceSpec defines the resource to upsert into the state.
type ResourceSpec struct {
	// URN is the fully qualified URN of the resource
	URN resource.URN

	// ID is the physical resource ID (empty for component resources)
	ID resource.ID

	// Type is the resource type token (e.g., "aws:s3/bucket:Bucket")
	Type string

	// Outputs is the PropertyMap containing all output values for the resource
	Outputs resource.PropertyMap
}

// UpsertOptions contains configuration for the upsert operation.
type UpsertOptions struct {
	// Resources is the list of resources to inject into state
	Resources []ResourceSpec

	// ProviderName is the name of the provider (e.g., "aws")
	ProviderName string

	// ProviderBinary is the path to the real provider binary for schema retrieval
	ProviderBinary string

	// WorkDir is the working directory containing the Pulumi program
	WorkDir string

	// StackName is the name of the stack to update
	StackName string

	// Port is the port for the mock provider server (0 for auto-assign)
	Port int
}

// UpsertResult contains the result of the upsert operation.
type UpsertResult struct {
	// Success indicates whether the operation succeeded
	Success bool

	// UpdatedResources lists the URNs of resources that were added to state
	UpdatedResources []resource.URN

	// Message provides additional information about the result
	Message string
}

// Upsert injects resources into the Pulumi state by running a targeted update
// with a mock provider. The mock provider responds to Create() calls with the
// provided outputs, allowing the resources to be added to state without actual
// cloud resource creation.
//
// The process:
// 1. Start a mock provider server on a specified port
// 2. Set PULUMI_DEBUG_PROVIDERS environment variable to point to the mock
// 3. Run `pulumi up --target <urn>` for each resource
// 4. The mock provider returns the predefined outputs during Create()
// 5. Pulumi records the resource in state with those outputs
//
// Example:
//
//	result, err := Upsert(ctx, UpsertOptions{
//	    Resources: []ResourceSpec{{
//	        URN: "urn:pulumi:dev::my-stack::aws:s3/bucket:Bucket::my-bucket",
//	        ID: "my-bucket-id",
//	        Type: "aws:s3/bucket:Bucket",
//	        Outputs: resource.PropertyMap{
//	            "bucket": resource.NewStringProperty("my-bucket-id"),
//	            "arn": resource.NewStringProperty("arn:aws:s3:::my-bucket-id"),
//	        },
//	    }},
//	    ProviderName: "aws",
//	    WorkDir: "/path/to/project",
//	    StackName: "dev",
//	})
func Upsert(ctx context.Context, opts UpsertOptions) (*UpsertResult, error) {
	if len(opts.Resources) == 0 {
		return nil, fmt.Errorf("no resources specified")
	}
	if opts.ProviderName == "" {
		return nil, fmt.Errorf("provider name is required")
	}
	if opts.WorkDir == "" {
		return nil, fmt.Errorf("working directory is required")
	}
	if opts.StackName == "" {
		return nil, fmt.Errorf("stack name is required")
	}

	// Start the mock provider server
	server, err := NewMockProviderServer(opts.ProviderName, opts.ProviderBinary, opts.Resources, opts.Port)
	if err != nil {
		return nil, fmt.Errorf("failed to create mock provider server: %w", err)
	}

	if err := server.Start(); err != nil {
		return nil, fmt.Errorf("failed to start mock provider server: %w", err)
	}
	defer server.Stop()

	// Run targeted updates for each resource
	updater := &targetedUpdater{
		workDir:   opts.WorkDir,
		stackName: opts.StackName,
		provider:  opts.ProviderName,
		port:      server.Port(),
	}

	updatedURNs, err := updater.updateResources(ctx, opts.Resources)
	if err != nil {
		return &UpsertResult{
			Success: false,
			Message: fmt.Sprintf("failed to update resources: %v", err),
		}, err
	}

	return &UpsertResult{
		Success:          true,
		UpdatedResources: updatedURNs,
		Message:          fmt.Sprintf("successfully upserted %d resources", len(updatedURNs)),
	}, nil
}
