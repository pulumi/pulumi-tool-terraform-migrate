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
	"os"

	"github.com/pulumi/pulumi/pkg/v3/backend/secrets"
	"github.com/pulumi/pulumi/pkg/v3/resource/deploy"
	"github.com/pulumi/pulumi/pkg/v3/resource/stack"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
)

// Helper to load stack state from a given Pulumi project.
func LoadStack(ctx context.Context, projectPath, stackName string) (*deploy.Snapshot, error) {
	// Create a LocalWorkspace pointing to the project
	ws, err := auto.NewLocalWorkspace(ctx, auto.WorkDir(projectPath))
	if err != nil {
		return nil, err
	}

	// Export the stack deployment
	deployment, err := ws.ExportStack(ctx, stackName)
	if err != nil {
		return nil, err
	}

	// Deserialize to a typed snapshot
	// Use DefaultProvider to allow loading secrets without decrypting them
	snapshot, err := stack.DeserializeUntypedDeployment(ctx, &deployment, secrets.DefaultProvider)
	if err != nil {
		return nil, err
	}

	return snapshot, nil
}

// LoadSnapshotFromState loads records records using an existing Stack reference.
func LoadSnapshotFromStack(ctx context.Context, s *auto.Stack) (*deploy.Snapshot, error) {
	// Set the passphrase in the process environment to avoid interactive prompts
	// This matches the passphrase used when creating the temp stack
	oldPassphrase := os.Getenv("PULUMI_CONFIG_PASSPHRASE")
	os.Setenv("PULUMI_CONFIG_PASSPHRASE", "test")
	defer func() {
		if oldPassphrase != "" {
			os.Setenv("PULUMI_CONFIG_PASSPHRASE", oldPassphrase)
		} else {
			os.Unsetenv("PULUMI_CONFIG_PASSPHRASE")
		}
	}()

	// Export the deployment
	deployment, err := s.Export(ctx)
	if err != nil {
		return nil, err
	}

	// Deserialize to snapshot
	// Use DefaultProvider to allow loading secrets without decrypting them
	snapshot, err := stack.DeserializeUntypedDeployment(ctx, &deployment, secrets.DefaultProvider)
	if err != nil {
		return nil, err
	}

	return snapshot, nil
}
