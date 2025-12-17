package pkg

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/pulumi/pulumi-terraform-migrate/pkg/tofu"
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
	tmpDir, err := os.MkdirTemp("", "tofu-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	statePath := createTestStateTfstate(t, tmpDir)

	terraformState, err := tofu.LoadTerraformState(statePath)
	require.NoError(t, err)

	pulumiProviders, err := GetPulumiProvidersForTerraformState(terraformState)
	require.NoError(t, err)

	require.Len(t, pulumiProviders, 1)
	randomProvider := pulumiProviders["registry.opentofu.org/hashicorp/random"]
	require.Equal(t, "random", randomProvider.Name)
	require.Greater(t, len(randomProvider.Resources), 2)
}
