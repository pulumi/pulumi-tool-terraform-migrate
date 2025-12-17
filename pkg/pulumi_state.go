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

type PulumiResource struct {
	ID      string
	Name    string
	Type    string
	Inputs  resource.PropertyMap
	Outputs resource.PropertyMap
}

type PulumiState struct {
	Providers []PulumiResource
	Resources []PulumiResource
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
	contract.Assertf(len(deployment.Resources) == 1, "expected stack resource in state, got %d", len(deployment.Resources))
	stackResource := deployment.Resources[0]

	now := time.Now()

	providerState := state.Providers[0]
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

	for _, res := range state.Resources {
		deployment.Resources = append(deployment.Resources, apitype.ResourceV3{
			URN:      makeUrn(stackName, projectName, res.Type, res.Name),
			Custom:   true,
			ID:       resource.ID(res.ID),
			Type:     tokens.Type(res.Type),
			Inputs:   res.Inputs.Mappable(),
			Outputs:  res.Outputs.Mappable(),
			Parent:   resource.URN(stackResource.URN),
			Provider: string(provider.URN) + "::" + string(provider.ID),
			Created:  &now,
			Modified: &now,
		})
	}

	return deployment, nil
}
