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

package pulumix

import (
	"context"
	"fmt"
	"os"

	"github.com/pulumi/pulumi/sdk/v3/go/auto/optimport"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
)

// An identifier to put in [id] for `pulumi import [type] [name] [id] [flags]` to import a Pulumi resource.
type ImportID string

// TypeScript, Python, etc.
type PulumiLanguage string

type SourceCode struct {
	Source   string
	Language PulumiLanguage
}

type ImportTask struct {
	ResourceType tokens.Type `json:"type"`
	ImportID     ImportID    `json:"id"`
	Name         string      `json:"name"`
}

// disambiguateImportTasks ensures unique (type, name) combinations by appending
// _1, _2, etc. to duplicate names within the same type.
func disambiguateImportTasks(tasks []ImportTask) []ImportTask {
	typeNameSeen := make(map[string]int) // key: "type:name", value: count of times seen
	result := make([]ImportTask, len(tasks))

	for i, task := range tasks {
		typeNameKey := string(task.ResourceType) + ":" + task.Name
		name := task.Name

		// Check if this (type, name) combination has been seen before
		if count, exists := typeNameSeen[typeNameKey]; exists {
			// Disambiguate by appending _1, _2, _3, etc.
			name = fmt.Sprintf("%s_%d", task.Name, count)
			typeNameSeen[typeNameKey] = count + 1
		} else {
			// First time seeing this combination
			typeNameSeen[typeNameKey] = 1
		}

		result[i] = ImportTask{
			ResourceType: task.ResourceType,
			ImportID:     task.ImportID,
			Name:         name,
		}
	}

	return result
}

// Uses a temporary Workspace to run `pulumi import --file import.json` and find the state and sources.
//
// TODO can expose some more functionality here, logical names, parent chains, suggested inputs even.
func ImportResourcesToPulumi(
	ctx context.Context,
	importTasks []ImportTask,
	language PulumiLanguage,
) ([]*resource.State, SourceCode, error) {
	if len(importTasks) == 0 {
		return nil, SourceCode{}, nil
	}

	// Create a temporary directory for the Pulumi project
	tmpDir, err := os.MkdirTemp("", "pulumi-import-*")
	if err != nil {
		return nil, SourceCode{}, fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a temporary project name and stack name
	projectName := "temp-import-project"
	stackName := "temp-stack"

	// Map language to Pulumi runtime
	var runtime string
	switch language {
	case "typescript":
		runtime = "nodejs"
	case "python":
		runtime = "python"
	case "go":
		runtime = "go"
	case "csharp", "dotnet":
		runtime = "dotnet"
	case "java":
		runtime = "java"
	case "yaml":
		runtime = "yaml"
	default:
		runtime = "nodejs" // default to nodejs
	}

	stack, err := NewTempStack(ctx, NewTempStackOptions{
		ProjectName: projectName,
		StackName:   stackName,
		TempDir:     tmpDir,
		Runtime:     runtime,
	})
	if err != nil {
		return nil, SourceCode{}, err
	}

	// Disambiguate tasks to ensure unique (type, name) combinations
	disambiguatedTasks := disambiguateImportTasks(importTasks)

	// Convert disambiguated tasks to optimport.ImportResource slice
	resources := make([]*optimport.ImportResource, len(disambiguatedTasks))
	for i, task := range disambiguatedTasks {
		resources[i] = &optimport.ImportResource{
			Type: string(task.ResourceType),
			Name: task.Name,
			ID:   string(task.ImportID),
		}
	}

	// Run the import with code generation enabled
	result, err := stack.ImportResources(ctx,
		optimport.Resources(resources),
		optimport.GenerateCode(true),
	)
	if err != nil {
		return nil, SourceCode{}, fmt.Errorf("failed to import resources: %w", err)
	}

	// Load the snapshot after import to get resource states
	snapshot, err := LoadSnapshotFromStack(ctx, &stack)
	if err != nil {
		return nil, SourceCode{}, fmt.Errorf("failed to load snapshot after import: %w", err)
	}

	// Build a map of resources by URN for efficient lookup
	resourcesByURN := make(map[resource.URN]*resource.State)
	if snapshot != nil && snapshot.Resources != nil {
		for _, res := range snapshot.Resources {
			if res != nil {
				resourcesByURN[res.URN] = res
			}
		}
	}

	// Match imported resources to the original import tasks in order
	// Use the disambiguated names to find resources in the snapshot
	states := make([]*resource.State, 0, len(disambiguatedTasks))

	for _, task := range disambiguatedTasks {
		// Find the matching resource by type and name
		// The URN format is: urn:pulumi:stack::project::type::name
		found := false
		for urn, res := range resourcesByURN {
			// Check if the resource type and name match
			if res.Type == task.ResourceType && string(urn.Name()) == task.Name {
				states = append(states, res)
				found = true
				break
			}
		}
		if !found {
			return nil, SourceCode{}, fmt.Errorf(
				"imported resource not found in snapshot: type=%s name=%s",
				task.ResourceType, task.Name)
		}
	}

	// Extract generated code from import result
	sourceCode := SourceCode{
		Source:   result.GeneratedCode,
		Language: language,
	}

	return states, sourceCode, nil
}
