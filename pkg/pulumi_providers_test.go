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
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tofu"
	"github.com/stretchr/testify/require"
)

// createTestStateTfstate creates a test state file in tfstate format
func createTestStateTfstate(t *testing.T, dir string) string {
	t.Helper()

	tfConfig := `
resource "random_string" "random" {
	length           = 16
	special          = true
	override_special = "/@£$"
}
`
	configPath := filepath.Join(dir, "main.tf")
	err := os.WriteFile(configPath, []byte(tfConfig), 0o644)
	require.NoError(t, err)

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

func TestGetPulumiProvidersForTerraformState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "tofu-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	statePath := createTestStateTfstate(t, tmpDir)

	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: statePath,
	})
	require.NoError(t, err)

	pulumiProviders, err := GetPulumiProvidersForTerraformState(tfState, nil)
	require.NoError(t, err)

	require.Len(t, pulumiProviders, 1)
	randomProvider := pulumiProviders["registry.opentofu.org/hashicorp/random"]
	require.Equal(t, "random", randomProvider.Name)
	require.Greater(t, len(randomProvider.Resources), 2)
}
