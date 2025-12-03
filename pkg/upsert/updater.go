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

package upsert

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
)

// targetedUpdater handles running pulumi up --target with the mock provider.
type targetedUpdater struct {
	workDir   string
	stackName string
	provider  string
	port      int
}

// updateResources runs pulumi up --target for each resource.
func (u *targetedUpdater) updateResources(ctx context.Context, resources []ResourceSpec) ([]resource.URN, error) {
	var updatedURNs []resource.URN

	for _, res := range resources {
		if err := u.updateResource(ctx, res); err != nil {
			return updatedURNs, fmt.Errorf("failed to update resource %s: %w", res.URN, err)
		}
		updatedURNs = append(updatedURNs, res.URN)
	}

	return updatedURNs, nil
}

// updateResource runs pulumi up --target for a single resource.
func (u *targetedUpdater) updateResource(ctx context.Context, res ResourceSpec) error {
	// Build the command
	cmd := exec.CommandContext(ctx, "pulumi", "up",
		"--target", string(res.URN),
		"--yes",
		"--skip-preview",
		"--stack", u.stackName,
	)

	cmd.Dir = u.workDir

	// Set PULUMI_DEBUG_PROVIDERS to point to our mock provider
	// Format: provider:port (e.g., "aws:12345")
	debugProviders := fmt.Sprintf("%s:%d", u.provider, u.port)

	// Preserve existing environment and add our debug setting
	cmd.Env = append(os.Environ(), fmt.Sprintf("PULUMI_DEBUG_PROVIDERS=%s", debugProviders))

	// Capture output
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pulumi up failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// VerifyStateUpdate checks if resources are present in the state after upsert.
func VerifyStateUpdate(ctx context.Context, workDir string, stackName string, urns []resource.URN) error {
	// Run pulumi stack export to get the current state
	cmd := exec.CommandContext(ctx, "pulumi", "stack", "export", "--stack", stackName)
	cmd.Dir = workDir

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to export stack: %w", err)
	}

	// Simple verification - check if URN appears in the exported state
	stateStr := string(output)
	for _, urn := range urns {
		if !contains(stateStr, string(urn)) {
			return fmt.Errorf("resource %s not found in state", urn)
		}
	}

	return nil
}

// VerifyPreviewClean checks that pulumi preview shows no changes.
func VerifyPreviewClean(ctx context.Context, workDir string, stackName string) error {
	cmd := exec.CommandContext(ctx, "pulumi", "preview", "--stack", stackName, "--expect-no-changes")
	cmd.Dir = workDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("preview shows changes: %w\nOutput: %s", err, string(output))
	}

	return nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && len(substr) > 0 && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
