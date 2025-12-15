package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hexops/autogold/v2"
	"github.com/pulumi/pulumi-terraform-migrate/pkg"
	"github.com/stretchr/testify/require"
)

func skipIfCI(t *testing.T) {
	t.Helper()
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping test in CI: TODO: set up pulumi credentials in CI")
	}
}

func runCommand(t *testing.T, dir string, command string, args ...string) string {
	t.Helper()
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("failed to run command %s %v, error: %v, output: %s", command, args, err, string(exitErr.Stderr))
		}
		t.Fatalf("failed to run command %s %v, error: %v", command, args, err)
	}
	return string(output)
}

func setupTFStack(t *testing.T, terraformSourcesPath string) string {
	dir, err := os.MkdirTemp("", "tf-stack-")
	require.NoError(t, err)
	t.Logf("Terraform stack directory: %s", dir)
	sourceDir := os.DirFS(terraformSourcesPath)
	os.CopyFS(dir, sourceDir)

	_ = runCommand(t, dir, "tofu", "init")
	_ = runCommand(t, dir, "tofu", "apply", "-auto-approve")
	return filepath.Join(dir, "terraform.tfstate")
}

func createPulumiStack(t *testing.T) string {
	dir, err := os.MkdirTemp("", "pulumi-stack-")
	require.NoError(t, err)
	t.Logf("Pulumi stack directory: %s", dir)

	_ = runCommand(t, dir, "pulumi", "new", "typescript", "--dir", dir, "--yes")
	_ = runCommand(t, dir, "pulumi", "up", "--yes")
	return dir
}

func TestTranslateBasic(t *testing.T) {
	skipIfCI(t)
	statePath := setupTFStack(t, "testdata/tf_random_stack")

	stackFolder := createPulumiStack(t)

	err := pkg.TranslateAndWriteState(statePath, stackFolder, filepath.Join(stackFolder, "state.json"))
	require.NoError(t, err)

	_ = runCommand(t, stackFolder, "pulumi", "stack", "import", "--file", filepath.Join(stackFolder, "state.json"))
	output := runCommand(t, stackFolder, "pulumi", "preview")

	autogold.ExpectFile(t, output)
}
