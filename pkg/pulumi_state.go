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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/contract"
)

func makeUrn(stackName, projectName, typeName, resourceName string) resource.URN {
	return resource.URN(fmt.Sprintf("urn:pulumi:%s::%s::%s::%s", stackName, projectName, typeName, resourceName))
}

// makeUrnWithParent creates a Pulumi URN with a parent type chain encoded via $ delimiters.
func makeUrnWithParent(stackName, projectName, parentTypeChain, typeName, resourceName string) resource.URN {
	fullType := typeName
	if parentTypeChain != "" {
		fullType = parentTypeChain + "$" + typeName
	}
	return resource.URN(fmt.Sprintf("urn:pulumi:%s::%s::%s::%s", stackName, projectName, fullType, resourceName))
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
	// For provider resources and components this is nil.
	Provider *PulumiResourceID

	// Parent type chain for URN encoding (e.g., "terraform:module/vpc:Vpc" or
	// "terraform:module/vpc:Vpc$terraform:module/subnets:Subnets").
	// Empty string means parent is Stack.
	Parent string
}

type PulumiState struct {
	Providers  []PulumiResource
	Components []PulumiResource
	Resources  []PulumiResource
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

func getProjectName(projectDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(projectDir, "Pulumi.yaml"))
	if err != nil {
		return "", fmt.Errorf("failed to read Pulumi.yaml: %w", err)
	}

	var project struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal(data, &project); err != nil {
		return "", fmt.Errorf("failed to parse Pulumi.yaml: %w", err)
	}
	if project.Name == "" {
		return "", fmt.Errorf("project name is empty in Pulumi.yaml")
	}
	return project.Name, nil
}


func InsertResourcesIntoDeployment(state *PulumiState, stackName, projectName string) (apitype.DeploymentV3, error) {
	if stackName == "" {
		return apitype.DeploymentV3{}, fmt.Errorf("stackName must not be empty")
	}
	if projectName == "" {
		return apitype.DeploymentV3{}, fmt.Errorf("projectName must not be empty")
	}

	now := time.Now()

	stackURN := makeUrn(stackName, projectName, "pulumi:pulumi:Stack", projectName+"-"+stackName)

	deployment := apitype.DeploymentV3{}
	deployment.Resources = append(deployment.Resources, apitype.ResourceV3{
		URN:  stackURN,
		Type: "pulumi:pulumi:Stack",
	})

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

	// Insert component resources (after providers, before custom resources).
	// Components are in depth-first order, so parents are always inserted before children.
	componentURNs := map[string]resource.URN{} // type chain -> URN

	for _, comp := range state.Components {
		urn := makeUrnWithParent(stackName, projectName, comp.Parent, comp.Type, comp.Name)

		parentURN := stackURN
		if comp.Parent != "" {
			if parentComponentURN, ok := componentURNs[comp.Parent]; ok {
				parentURN = parentComponentURN
			}
		}

		// Register this component's full type chain for child lookups
		fullTypeChain := comp.Type
		if comp.Parent != "" {
			fullTypeChain = comp.Parent + "$" + comp.Type
		}
		componentURNs[fullTypeChain] = urn

		inputs := comp.Inputs
		if inputs == nil {
			inputs = resource.PropertyMap{}
		}
		outputs := comp.Outputs
		if outputs == nil {
			outputs = resource.PropertyMap{}
		}

		deployment.Resources = append(deployment.Resources, apitype.ResourceV3{
			URN:      urn,
			Custom:   false,
			Type:     tokens.Type(comp.Type),
			Inputs:   inputs.Mappable(),
			Outputs:  outputs.Mappable(),
			Parent:   parentURN,
			Created:  &now,
			Modified: &now,
		})
	}

	for _, res := range state.Resources {
		contract.Assertf(res.Provider != nil, "Expected a provider association for a custom resource")

		providerRecord, err := state.FindProvider(*res.Provider)
		if err != nil {
			return apitype.DeploymentV3{}, err
		}

		providerURN := makeUrn(stackName, projectName, providerRecord.Type, providerRecord.Name)
		providerLink := fmt.Sprintf("%s::%s", providerURN, providerRecord.ID)

		urn := makeUrnWithParent(stackName, projectName, res.Parent, res.Type, res.Name)

		parentURN := stackURN
		if res.Parent != "" {
			if parentComponentURN, ok := componentURNs[res.Parent]; ok {
				parentURN = parentComponentURN
			}
		}

		deployment.Resources = append(deployment.Resources, apitype.ResourceV3{
			URN:      urn,
			Custom:   true,
			ID:       resource.ID(res.ID),
			Type:     tokens.Type(res.Type),
			Inputs:   res.Inputs.Mappable(),
			Outputs:  res.Outputs.Mappable(),
			Parent:   parentURN,
			Provider: providerLink,
			Created:  &now,
			Modified: &now,
		})
	}

	return deployment, nil
}

