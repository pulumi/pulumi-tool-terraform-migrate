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
	"path/filepath"
	"strings"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreview_CreateResources(t *testing.T) {
	// YAML program that creates new resources
	yamlProgram := `
resources:
  test-string:
    type: random:RandomString
    properties:
      length: 16

  test-key:
    type: tls:PrivateKey
    properties:
      algorithm: RSA
      rsaBits: 2048
`

	stack, cleanup := setupLocalStack(t, "test-create", "dev", yamlProgram)
	defer cleanup()

	ctx := context.Background()

	// Run preview on fresh stack (should create resources)
	statusMap, err := Preview(ctx, stack, nil)
	require.NoError(t, err)
	require.NotEmpty(t, statusMap)

	// Count operations
	createCount := 0
	for _, status := range statusMap {
		if status == Create {
			createCount++
		}
	}

	// Should have at least 2 creates (random string + tls key)
	assert.GreaterOrEqual(t, createCount, 2, "Expected at least 2 create operations")

	// Verify specific resources
	foundRandom := false
	foundTls := false
	for urn, status := range statusMap {
		if status == Create {
			urnStr := string(urn)
			if strings.Contains(urnStr, "random:index/randomString:RandomString") {
				foundRandom = true
			}
			if strings.Contains(urnStr, "tls:index/privateKey:PrivateKey") {
				foundTls = true
			}
		}
	}

	assert.True(t, foundRandom, "Expected to find random string resource")
	assert.True(t, foundTls, "Expected to find TLS private key resource")
}

func TestPreview_NoChanges(t *testing.T) {
	yamlProgram := `
resources:
  stable-string:
    type: random:RandomString
    properties:
      length: 16
`

	stack, cleanup := setupLocalStack(t, "test-nochange", "dev", yamlProgram)
	defer cleanup()

	ctx := context.Background()

	// Deploy first
	_, err := stack.Up(ctx)
	require.NoError(t, err)

	// Run preview again (should show no changes)
	statusMap, err := Preview(ctx, stack, nil)
	require.NoError(t, err)

	// All resources should be OpSame
	for urn, status := range statusMap {
		if status != Same {
			t.Errorf("Expected no changes, but got operation %s for %s", status, urn)
		}
	}
}

func TestPreview_ReplaceResources(t *testing.T) {
	// Initial YAML program
	initialYaml := `
resources:
  update-string:
    type: random:RandomString
    properties:
      length: 16
`

	stack, cleanup := setupLocalStack(t, "test-update", "dev", initialYaml)
	defer cleanup()

	ctx := context.Background()

	// Deploy initial state
	_, err := stack.Up(ctx)
	require.NoError(t, err)

	// Updated YAML program (changed length, forces replacement)
	updatedYaml := `
resources:
  update-string:
    type: random:RandomString
    properties:
      length: 32
      special: false
`

	// Write updated Pulumi.yaml
	ws := stack.Workspace()
	workDir := ws.WorkDir()
	pulumiYaml := `name: test-update
runtime: yaml
backend:
  url: ` + "file://" + filepath.Dir(workDir) + "/state" + `
` + updatedYaml
	err = os.WriteFile(filepath.Join(workDir, "Pulumi.yaml"), []byte(pulumiYaml), 0644)
	require.NoError(t, err)

	// Run preview (should show replace since length change forces replacement)
	statusMap, err := Preview(ctx, stack, nil)
	require.NoError(t, err)

	// Should have replace operations
	replaceCount := 0
	for _, status := range statusMap {
		if status == Replace {
			replaceCount++
		}
	}

	assert.Greater(t, replaceCount, 0, "Expected at least one replace operation")
}

func TestPreview_DeleteResources(t *testing.T) {
	// YAML with multiple resources
	initialYaml := `
resources:
  keep-me:
    type: random:RandomString
    properties:
      length: 16

  delete-me:
    type: random:RandomString
    properties:
      length: 16
`

	stack, cleanup := setupLocalStack(t, "test-delete", "dev", initialYaml)
	defer cleanup()

	ctx := context.Background()

	// Deploy initial state
	_, err := stack.Up(ctx)
	require.NoError(t, err)

	// Updated YAML with one resource removed
	updatedYaml := `
resources:
  keep-me:
    type: random:RandomString
    properties:
      length: 16
`

	// Write updated Pulumi.yaml
	ws := stack.Workspace()
	workDir := ws.WorkDir()
	pulumiYaml := `name: test-delete
runtime: yaml
backend:
  url: ` + "file://" + filepath.Dir(workDir) + "/state" + `
` + updatedYaml
	err = os.WriteFile(filepath.Join(workDir, "Pulumi.yaml"), []byte(pulumiYaml), 0644)
	require.NoError(t, err)

	// Run preview (should show delete)
	statusMap, err := Preview(ctx, stack, nil)
	require.NoError(t, err)

	// Should have delete operation
	deleteCount := 0
	for urn, status := range statusMap {
		if status == Delete {
			deleteCount++
			assert.Contains(t, string(urn), "delete-me", "Wrong resource marked for deletion")
		}
	}

	assert.Greater(t, deleteCount, 0, "Expected at least one delete operation")
}

func TestPreview_UpdateResources(t *testing.T) {
	// Initial YAML program using Command provider
	initialYaml := `
resources:
  my-command:
    type: command:local:Command
    properties:
      create: echo "version1"
`

	stack, cleanup := setupLocalStack(t, "test-update-cmd", "dev", initialYaml)
	defer cleanup()

	ctx := context.Background()

	// Deploy initial state
	_, err := stack.Up(ctx)
	require.NoError(t, err)

	// Updated YAML program (changed command, should update in-place)
	updatedYaml := `
resources:
  my-command:
    type: command:local:Command
    properties:
      create: echo "version2"
      update: echo "updated"
`

	// Write updated Pulumi.yaml
	ws := stack.Workspace()
	workDir := ws.WorkDir()
	pulumiYaml := `name: test-update-cmd
runtime: yaml
backend:
  url: ` + "file://" + filepath.Dir(workDir) + "/state" + `
` + updatedYaml
	err = os.WriteFile(filepath.Join(workDir, "Pulumi.yaml"), []byte(pulumiYaml), 0644)
	require.NoError(t, err)

	// Run preview (should show update, not replace)
	statusMap, err := Preview(ctx, stack, nil)
	require.NoError(t, err)

	urn := "urn:pulumi:dev::test-update-cmd::command:local:Command::my-command"
	require.Equal(t, Update, statusMap[resource.URN(urn)])
}

func TestPreview_WithCustomOptions(t *testing.T) {
	yamlProgram := `
resources:
  test-options:
    type: random:RandomString
    properties:
      length: 16
`

	stack, cleanup := setupLocalStack(t, "test-options", "dev", yamlProgram)
	defer cleanup()

	ctx := context.Background()

	// Test with custom buffer size and additional options
	opts := &PreviewOptions{
		AdditionalOptions: []optpreview.Option{
			optpreview.Message("Custom preview message"),
		},
	}

	statusMap, err := Preview(ctx, stack, opts)
	require.NoError(t, err)
	require.NotEmpty(t, statusMap)
}

// setupLocalStack creates a temporary workspace with local backend using YAML project
func setupLocalStack(t *testing.T, projectName, stackName, yamlProgram string) (auto.Stack, func()) {
	ctx := context.Background()

	// Set passphrase for local backend
	oldPassphrase := os.Getenv("PULUMI_CONFIG_PASSPHRASE")
	err := os.Setenv("PULUMI_CONFIG_PASSPHRASE", "test-passphrase")
	require.NoError(t, err)

	// Create temporary directory for the local backend
	tmpDir, err := os.MkdirTemp("", "pulumi-test-*")
	require.NoError(t, err)

	workDir := filepath.Join(tmpDir, "workspace")
	err = os.MkdirAll(workDir, 0755)
	require.NoError(t, err)

	// Create state directory
	stateDir := filepath.Join(tmpDir, "state")
	err = os.MkdirAll(stateDir, 0755)
	require.NoError(t, err)

	// Write Pulumi.yaml
	pulumiYaml := `name: ` + projectName + `
runtime: yaml
backend:
  url: file://` + stateDir + `
` + yamlProgram
	err = os.WriteFile(filepath.Join(workDir, "Pulumi.yaml"), []byte(pulumiYaml), 0644)
	require.NoError(t, err)

	// Create workspace
	ws, err := auto.NewLocalWorkspace(ctx, auto.WorkDir(workDir))
	require.NoError(t, err)

	// Install required plugins
	err = ws.InstallPlugin(ctx, "random", "v4.16.7")
	require.NoError(t, err)

	err = ws.InstallPlugin(ctx, "tls", "v5.0.8")
	require.NoError(t, err)

	err = ws.InstallPlugin(ctx, "command", "v1.0.1")
	require.NoError(t, err)

	// Create or select stack
	stack, err := auto.UpsertStack(ctx, stackName, ws)
	require.NoError(t, err)

	cleanup := func() {
		os.RemoveAll(tmpDir)
		if oldPassphrase != "" {
			os.Setenv("PULUMI_CONFIG_PASSPHRASE", oldPassphrase)
		} else {
			os.Unsetenv("PULUMI_CONFIG_PASSPHRASE")
		}
	}

	return stack, cleanup
}
