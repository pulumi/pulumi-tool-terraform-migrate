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
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/contract"
)

func makeUrn(stackName, projectName, typeName, resourceName string) resource.URN {
	return resource.URN(fmt.Sprintf("urn:pulumi:%s::%s::%s::%s", stackName, projectName, typeName, resourceName))
}

// Identifier within a stack.
type PulumiResourceID struct {
	ID   string
	Name string
	Type string
}

func (p PulumiResourceID) Equal(other PulumiResourceID) bool {
	return p.ID == other.ID && p.Name == other.Name && p.Type == other.Type
}

type PulumiResource struct {
	PulumiResourceID

	Inputs  resource.PropertyMap
	Outputs resource.PropertyMap

	// For resources this identifies the associated provider.
	//
	// For provider resources this nil.
	Provider *PulumiResourceID
}

type PulumiState struct {
	Providers []PulumiResource
	Resources []PulumiResource
}

func (st PulumiState) FindProvider(identity PulumiResourceID) (PulumiResource, error) {
	for _, p := range st.Providers {
		if p.PulumiResourceID.Equal(identity) {
			return p, nil
		}
	}
	return PulumiResource{}, fmt.Errorf("No providers found with ID=%q Name=%q Type=%q",
		identity.ID, identity.Name, identity.Type)
}

func getStackName(projectFolder string) (string, error) {
	cmd := exec.Command("pulumi", "stack", "ls", "--json")
	cmd.Dir = projectFolder
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get stack name: %w", err)
	}

	var stacks []struct {
		Name    string `json:"name"`
		Current bool   `json:"current"`
	}
	err = json.Unmarshal(output, &stacks)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal stack list: %w", err)
	}
	if len(stacks) == 0 {
		return "", fmt.Errorf("no stacks found")
	}

	for _, stack := range stacks {
		if stack.Current {
			return stack.Name, nil
		}
	}
	return "", fmt.Errorf("no current stack found")
}

type DeploymentResult struct {
	Deployment  apitype.DeploymentV3
	ProjectName string
	StackName   string
}

func GetDeployment(outputFolder string) (*DeploymentResult, error) {
	ctx := context.Background()
	workspace, err := auto.NewLocalWorkspace(ctx, auto.WorkDir(outputFolder))
	if err != nil {
		return nil, fmt.Errorf("failed to create workspace: %w", err)
	}

	// TODO[pulumi/pulumi#21266]: Use automation API to get the selected stack name once the issue is fixed.
	stackName, err := getStackName(outputFolder)
	if err != nil {
		return nil, fmt.Errorf("failed to get stack name: %w", err)
	}

	untypedDeployment, err := workspace.ExportStack(ctx, stackName)
	if err != nil {
		return nil, fmt.Errorf("failed to export stack: %w", err)
	}

	deployment := apitype.DeploymentV3{}
	err = json.Unmarshal(untypedDeployment.Deployment, &deployment)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal stack deployment: %w", err)
	}

	projectSettings, err := workspace.ProjectSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get project settings: %w", err)
	}

	if projectSettings == nil {
		return nil, fmt.Errorf("project settings are nil")
	}

	return &DeploymentResult{
		Deployment:  deployment,
		ProjectName: string(projectSettings.Name),
		StackName:   stackName,
	}, nil
}

func InsertResourcesIntoDeployment(state *PulumiState, stackName, projectName string, deployment apitype.DeploymentV3) (apitype.DeploymentV3, error) {
	nres := len(deployment.Resources)

	if nres == 0 {
		return apitype.DeploymentV3{}, fmt.Errorf(
			"No Stack resource found in the Pulumi state for stack '%q'. "+
				"Please run `pulumi up` to populate the initial Pulumi state and configure secrets providers, then try again.",
			stackName)
	}

	if nres > 1 {
		return apitype.DeploymentV3{}, fmt.Errorf(
			"Found %d resources in stack %q, expected 1 (Stack resource). "+
				"Migrating resources into stacks with pre-existing resources is not yet supported",
			nres, stackName)
	}

	now := time.Now()

	stackResource, err := findStackResource(deployment)
	if err != nil {
		return apitype.DeploymentV3{}, err
	}

	for _, providerState := range state.Providers {
		provider := apitype.ResourceV3{
			URN:      makeUrn(stackName, projectName, providerState.Type, providerState.Name),
			Custom:   true,
			ID:       resource.ID(providerState.ID),
			Type:     tokens.Type(providerState.Type),
			Inputs:   providerState.Inputs.Mappable(),
			Outputs:  providerState.Outputs.Mappable(),
			Created:  &now,
			Modified: &now,
		}
		deployment.Resources = append(deployment.Resources, provider)
	}

	for _, res := range state.Resources {
		contract.Assertf(res.Provider != nil, "Expected a provider association for a custom resource")

		providerRecord, err := state.FindProvider(*res.Provider)
		if err != nil {
			return apitype.DeploymentV3{}, err
		}

		providerURN := makeUrn(stackName, projectName, providerRecord.Type, providerRecord.Name)
		providerLink := fmt.Sprintf("%s::%s", providerURN, providerRecord.ID)

		deployment.Resources = append(deployment.Resources, apitype.ResourceV3{
			URN:      makeUrn(stackName, projectName, res.Type, res.Name),
			Custom:   true,
			ID:       resource.ID(res.ID),
			Type:     tokens.Type(res.Type),
			Inputs:   res.Inputs.Mappable(),
			Outputs:  res.Outputs.Mappable(),
			Parent:   resource.URN(stackResource.URN),
			Provider: providerLink,
			Created:  &now,
			Modified: &now,
		})
	}

	return deployment, nil
}

func findStackResource(deployment apitype.DeploymentV3) (apitype.ResourceV3, error) {
	for _, r := range deployment.Resources {
		if string(r.URN.QualifiedType()) == "pulumi:pulumi:Stack" {
			return r, nil
		}
	}
	return apitype.ResourceV3{}, fmt.Errorf("No resources with qualified type pulumi:pulumi:Stack")
}
