package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hexops/autogold/v2"
	"github.com/pulumi/pulumi-terraform-migrate/pkg"
	"github.com/stretchr/testify/require"
)

func runCommand(t *testing.T, dir string, command string, args ...string) string {
	t.Helper()
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("failed to run command %s %v, error: %v, output: %s, stdout: %s", command, args, err, string(exitErr.Stderr), output)
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

	t.Cleanup(func() {
		_ = runCommand(t, dir, "tofu", "destroy", "-auto-approve")
	})
	return filepath.Join(dir, "terraform.tfstate")
}

func createPulumiStack(t *testing.T) (string, string) {
	dir, err := os.MkdirTemp("", "pulumi-stack-")
	require.NoError(t, err)
	t.Logf("Pulumi stack directory: %s", dir)

	_ = runCommand(t, dir, "pulumi", "new", "typescript", "--dir", dir, "--yes")
	_ = runCommand(t, dir, "pulumi", "up", "--yes")

	stackName := filepath.Base(dir)
	return dir, stackName
}

func replacePackageJson(t *testing.T, stackFolder string, stackName string, packageJsonPath string) {
	t.Helper()
	err := os.Remove(filepath.Join(stackFolder, "package.json"))
	require.NoError(t, err)
	packageJsonBytes, err := os.ReadFile(packageJsonPath)
	packageJsonString := string(packageJsonBytes)
	require.NoError(t, err)
	packageJsonString = strings.Replace(packageJsonString, "PLACEHOLDER", stackName, 1)
	err = os.WriteFile(filepath.Join(stackFolder, "package.json"), []byte(packageJsonString), 0o600)
	require.NoError(t, err)
}

func replaceIndexTs(t *testing.T, stackFolder string, indexTsPath string) {
	t.Helper()
	err := os.Remove(filepath.Join(stackFolder, "index.ts"))
	require.NoError(t, err)
	err = os.Link(indexTsPath, filepath.Join(stackFolder, "index.ts"))
	require.NoError(t, err)
}

func TestTranslateBasic(t *testing.T) {
	statePath := setupTFStack(t, "testdata/tf_random_stack")
	stackFolder, stackName := createPulumiStack(t)

	err := pkg.TranslateAndWriteState(statePath, stackFolder, filepath.Join(stackFolder, "state.json"))
	require.NoError(t, err)

	_ = runCommand(t, stackFolder, "pulumi", "stack", "import", "--file", filepath.Join(stackFolder, "state.json"))

	replaceIndexTs(t, stackFolder, filepath.Join("testdata/pulumi_random_stack", "index.ts"))
	replacePackageJson(t, stackFolder, stackName, filepath.Join("testdata/pulumi_random_stack", "package.json"))
	_ = runCommand(t, stackFolder, "pulumi", "install")
	output := runCommand(t, stackFolder, "pulumi", "preview", "--diff")

	autogold.ExpectFile(t, output)
}

func TestTranslateBasicWithEdit(t *testing.T) {
	statePath := setupTFStack(t, "testdata/tf_random_stack")
	stackFolder, stackName := createPulumiStack(t)

	err := pkg.TranslateAndWriteState(statePath, stackFolder, filepath.Join(stackFolder, "state.json"))
	require.NoError(t, err)

	_ = runCommand(t, stackFolder, "pulumi", "stack", "import", "--file", filepath.Join(stackFolder, "state.json"))

	// This changes the length of the random string from 16 to 17 in order to test that edits to resources still work.
	replaceIndexTs(t, stackFolder, filepath.Join("testdata/pulumi_random_stack2", "index.ts"))
	replacePackageJson(t, stackFolder, stackName, filepath.Join("testdata/pulumi_random_stack2", "package.json"))
	_ = runCommand(t, stackFolder, "pulumi", "install")

	output := runCommand(t, stackFolder, "pulumi", "preview", "--diff")
	autogold.ExpectFile(t, output, autogold.Name("TestTranslateBasicWithEdit-preview"))

	output = runCommand(t, stackFolder, "pulumi", "up", "--yes")

	autogold.ExpectFile(t, output, autogold.Name("TestTranslateBasicWithEdit-up"))
}

func TestTranslateWithDependency(t *testing.T) {
	statePath := setupTFStack(t, "testdata/tf_dependency_stack")
	stackFolder, stackName := createPulumiStack(t)

	err := pkg.TranslateAndWriteState(statePath, stackFolder, filepath.Join(stackFolder, "state.json"))
	require.NoError(t, err)

	_ = runCommand(t, stackFolder, "pulumi", "stack", "import", "--file", filepath.Join(stackFolder, "state.json"))

	replacePackageJson(t, stackFolder, stackName, filepath.Join("testdata/pulumi_dependency_stack", "package.json"))
	replaceIndexTs(t, stackFolder, filepath.Join("testdata/pulumi_dependency_stack", "index.ts"))
	_ = runCommand(t, stackFolder, "pulumi", "install")

	output := runCommand(t, stackFolder, "pulumi", "up", "--yes")
	autogold.ExpectFile(t, output)
}

func TestTranslateAWSStack(t *testing.T) {
	statePath := setupTFStack(t, "testdata/tf_aws_stack")
	stackFolder, stackName := createPulumiStack(t)

	err := pkg.TranslateAndWriteState(statePath, stackFolder, filepath.Join(stackFolder, "state.json"))
	require.NoError(t, err)

	_ = runCommand(t, stackFolder, "pulumi", "stack", "import", "--file", filepath.Join(stackFolder, "state.json"))

	replacePackageJson(t, stackFolder, stackName, filepath.Join("testdata/pulumi_aws_stack", "package.json"))
	replaceIndexTs(t, stackFolder, filepath.Join("testdata/pulumi_aws_stack", "index.ts"))
	_ = runCommand(t, stackFolder, "pulumi", "install")
	// TODO: Why do BucketLifecycleConfiguration and RolePolicy produce diffs.
	output := runCommand(t, stackFolder, "pulumi", "preview", "--diff")

	autogold.ExpectFile(t, output)
}

func TestTranslateAWSStackWithEdit(t *testing.T) {
	statePath := setupTFStack(t, "testdata/tf_aws_stack")
	stackFolder, stackName := createPulumiStack(t)

	err := pkg.TranslateAndWriteState(statePath, stackFolder, filepath.Join(stackFolder, "state.json"))
	require.NoError(t, err)

	_ = runCommand(t, stackFolder, "pulumi", "stack", "import", "--file", filepath.Join(stackFolder, "state.json"))

	replacePackageJson(t, stackFolder, stackName, filepath.Join("testdata/pulumi_aws_stack", "package.json"))
	replaceIndexTs(t, stackFolder, filepath.Join("testdata/pulumi_aws_stack", "index.ts"))
	_ = runCommand(t, stackFolder, "pulumi", "install")
	output := runCommand(t, stackFolder, "pulumi", "preview", "--diff")

	autogold.ExpectFile(t, output, autogold.Name("TestTranslateAWSStackWithEdit-preview"))

	output = runCommand(t, stackFolder, "pulumi", "up", "--yes")
	autogold.ExpectFile(t, output, autogold.Name("TestTranslateAWSStackWithEdit-up1"))

	replacePackageJson(t, stackFolder, stackName, filepath.Join("testdata/pulumi_aws_stack2", "package.json"))
	replaceIndexTs(t, stackFolder, filepath.Join("testdata/pulumi_aws_stack2", "index.ts"))

	output = runCommand(t, stackFolder, "pulumi", "up", "--yes")
	autogold.ExpectFile(t, output, autogold.Name("TestTranslateAWSStackWithEdit-up2"))
}
