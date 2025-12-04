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

package tofu

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipIfTofuNotAvailable skips the test if tofu is not in PATH
func skipIfTofuNotAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skip("tofu binary not found in PATH, skipping test")
	}
}

// createTestStateJSON creates a test state file in JSON format
func createTestStateJSON(t *testing.T, dir string) string {
	t.Helper()

	state := &tfjson.State{
		FormatVersion:    "1.0",
		TerraformVersion: "1.5.0",
		Values: &tfjson.StateValues{
			RootModule: &tfjson.StateModule{
				Resources: []*tfjson.StateResource{
					{
						Address: "null_resource.test",
						Mode:    tfjson.ManagedResourceMode,
						Type:    "null_resource",
						Name:    "test",
					},
				},
			},
		},
	}

	data, err := json.MarshalIndent(state, "", "  ")
	require.NoError(t, err)

	path := filepath.Join(dir, "test.json")
	err = os.WriteFile(path, data, 0644)
	require.NoError(t, err)

	return path
}

// createTestStateTfstate creates a test state file in tfstate format
func createTestStateTfstate(t *testing.T, dir string) string {
	t.Helper()
	skipIfTofuNotAvailable(t)

	// Create a minimal Terraform configuration
	tfConfig := `
resource "null_resource" "test" {
}
`
	configPath := filepath.Join(dir, "main.tf")
	err := os.WriteFile(configPath, []byte(tfConfig), 0644)
	require.NoError(t, err)

	// Initialize and create state
	cmd := exec.Command("tofu", "init")
	cmd.Dir = dir
	err = cmd.Run()
	require.NoError(t, err)

	cmd = exec.Command("tofu", "apply", "-auto-approve")
	cmd.Dir = dir
	err = cmd.Run()
	require.NoError(t, err)

	statePath := filepath.Join(dir, "terraform.tfstate")
	_, err = os.Stat(statePath)
	require.NoError(t, err)

	return statePath
}

func TestReadTerraformStateJSON(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tofu-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	statePath := createTestStateJSON(t, tmpDir)

	state, err := ReadTerraformStateJSON(statePath)
	require.NoError(t, err)
	assert.NotNil(t, state)
	assert.Equal(t, "1.0", state.FormatVersion)
	assert.Equal(t, "1.5.0", state.TerraformVersion)
	assert.NotNil(t, state.Values)
	assert.NotNil(t, state.Values.RootModule)
	assert.Len(t, state.Values.RootModule.Resources, 1)
	assert.Equal(t, "null_resource.test", state.Values.RootModule.Resources[0].Address)
}

func TestReadTerraformStateJSON_InvalidFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tofu-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Test with non-existent file
	_, err = ReadTerraformStateJSON(filepath.Join(tmpDir, "nonexistent.json"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read state file")

	// Test with invalid JSON
	invalidPath := filepath.Join(tmpDir, "invalid.json")
	err = os.WriteFile(invalidPath, []byte("not valid json"), 0644)
	require.NoError(t, err)

	_, err = ReadTerraformStateJSON(invalidPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse state JSON")
}

func TestLoadTerraformState_JSON(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tofu-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	statePath := createTestStateJSON(t, tmpDir)

	state, err := LoadTerraformState(statePath)
	require.NoError(t, err)
	assert.NotNil(t, state)
	assert.Equal(t, "1.0", state.FormatVersion)
	assert.Equal(t, "1.5.0", state.TerraformVersion)
}

func TestLoadTerraformState_Tfstate(t *testing.T) {
	skipIfTofuNotAvailable(t)

	tmpDir, err := os.MkdirTemp("", "tofu-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	statePath := createTestStateTfstate(t, tmpDir)

	state, err := LoadTerraformState(statePath)
	require.NoError(t, err)
	assert.NotNil(t, state)
	assert.NotEmpty(t, state.FormatVersion)
	assert.NotEmpty(t, state.TerraformVersion)
	assert.NotNil(t, state.Values)
}

func TestLoadTerraformState_UnsupportedFormat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tofu-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a file with unsupported extension
	unsupportedPath := filepath.Join(tmpDir, "state.txt")
	err = os.WriteFile(unsupportedPath, []byte("test"), 0644)
	require.NoError(t, err)

	_, err = LoadTerraformState(unsupportedPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported state file format")
}

func TestLoadBinaryStateWithTofu(t *testing.T) {
	skipIfTofuNotAvailable(t)

	tmpDir, err := os.MkdirTemp("", "tofu-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	statePath := createTestStateTfstate(t, tmpDir)

	state, err := loadBinaryStateWithTofu(statePath)
	require.NoError(t, err)
	assert.NotNil(t, state)
	assert.NotEmpty(t, state.FormatVersion)
	assert.NotNil(t, state.Values)
}

func TestLoadBinaryStateWithTofu_NonExistentFile(t *testing.T) {
	skipIfTofuNotAvailable(t)

	tmpDir, err := os.MkdirTemp("", "tofu-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	nonExistentPath := filepath.Join(tmpDir, "nonexistent.tfstate")

	_, err = loadBinaryStateWithTofu(nonExistentPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to convert binary state file")
}
