package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hexops/autogold/v2"
	"github.com/pulumi/pulumi-terraform-migrate/pkg"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/stretchr/testify/require"
)

// runCommand is a helper function to run commands like tofu/terraform.
// NOTE: For Pulumi commands, use the Automation API instead.
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

func setupTFStack(t *testing.T, terraformSourcesPath string, cacheFolder string) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "tf-stack-")
	require.NoError(t, err)
	t.Logf("Terraform stack directory: %s", dir)
	sourceDir := os.DirFS(terraformSourcesPath)
	os.CopyFS(dir, sourceDir)

	_ = runCommand(t, dir, "tofu", "init")

	// Check for cached state
	if cacheFolder != "" {
		cacheDir := filepath.Join("testdata", cacheFolder)
		cachedStatePath := filepath.Join(cacheDir, "terraform.tfstate")
		if _, err := os.Stat(cachedStatePath); err == nil {
			t.Logf("Using cached terraform.tfstate from %s", cachedStatePath)
			// Copy cached state to temp directory
			stateContent, err := os.ReadFile(cachedStatePath)
			require.NoError(t, err)
			err = os.WriteFile(filepath.Join(dir, "terraform.tfstate"), stateContent, 0644)
			require.NoError(t, err)
			return filepath.Join(dir, "terraform.tfstate")
		}
	}

	_ = runCommand(t, dir, "tofu", "apply", "-auto-approve")

	// Cache the state file if cacheFolder is specified
	if cacheFolder != "" {
		cacheDir := filepath.Join("testdata", cacheFolder)
		err = os.MkdirAll(cacheDir, 0755)
		require.NoError(t, err)
		cachedStatePath := filepath.Join(cacheDir, "terraform.tfstate")
		stateContent, err := os.ReadFile(filepath.Join(dir, "terraform.tfstate"))
		require.NoError(t, err)
		err = os.WriteFile(cachedStatePath, stateContent, 0644)
		require.NoError(t, err)
		t.Logf("Cached terraform.tfstate to %s", cachedStatePath)
	}

	t.Cleanup(func() {
		_ = runCommand(t, dir, "tofu", "destroy", "-auto-approve")
	})
	return filepath.Join(dir, "terraform.tfstate")
}

func createPulumiStack(t *testing.T) auto.Workspace {
	t.Helper()

	// Create a temporary directory for the test project
	dir, err := os.MkdirTemp("", "pulumi-stack-")
	require.NoError(t, err)
	t.Logf("Pulumi stack directory: %s", dir)

	// Set up state backend directory
	stateDir := filepath.Join(dir, ".pulumi")
	err = os.MkdirAll(stateDir, 0755)
	require.NoError(t, err)

	stackName := filepath.Base(dir)

	// Create the TypeScript project using pulumi new with filestate backend
	cmd := exec.Command("pulumi", "new", "typescript", "--dir", dir, "--yes", "--force", "-s", stackName)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PULUMI_BACKEND_URL=file://"+stateDir,
		"PULUMI_CONFIG_PASSPHRASE=test",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run pulumi new: %v, output: %s", err, string(output))
	}

	// Create workspace using Automation API with same backend configuration
	ctx := context.Background()
	workspace, err := auto.NewLocalWorkspace(ctx,
		auto.WorkDir(dir),
		auto.EnvVars(map[string]string{
			"PULUMI_BACKEND_URL":       "file://" + stateDir,
			"PULUMI_CONFIG_PASSPHRASE": "test",
		}),
	)
	require.NoError(t, err)

	// Get the stack that was created by pulumi new
	stack, err := auto.SelectStack(ctx, stackName, workspace)
	require.NoError(t, err)

	// Run initial up
	_, err = stack.Up(ctx)
	require.NoError(t, err)

	return workspace
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

func importStackState(t *testing.T, ctx context.Context, stack auto.Stack, statePath string) {
	t.Helper()
	stateBytes, err := os.ReadFile(statePath)
	require.NoError(t, err)

	var deployment apitype.UntypedDeployment
	err = json.Unmarshal(stateBytes, &deployment)
	require.NoError(t, err)

	err = stack.Import(ctx, deployment)
	require.NoError(t, err)
}

func TestTranslateBasic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	statePath := setupTFStack(t, "testdata/tf_random_stack", "tf_random_stack_cache")
	workspace := createPulumiStack(t)
	stackFolder := workspace.WorkDir()
	stackSummary, err := workspace.Stack(ctx)
	require.NoError(t, err)
	stackName := stackSummary.Name

	err = pkg.TranslateAndWriteStateWithWorkspace(statePath, workspace, filepath.Join(stackFolder, "state.json"))
	require.NoError(t, err)

	stack, err := auto.SelectStack(ctx, stackName, workspace)
	require.NoError(t, err)

	importStackState(t, ctx, stack, filepath.Join(stackFolder, "state.json"))

	replaceIndexTs(t, stackFolder, filepath.Join("testdata/pulumi_random_stack", "index.ts"))
	replacePackageJson(t, stackFolder, stackName, filepath.Join("testdata/pulumi_random_stack", "package.json"))

	err = workspace.Install(ctx, nil)
	require.NoError(t, err)

	result, err := stack.Preview(ctx, optpreview.Diff())
	require.NoError(t, err)

	autogold.Expect(map[apitype.OpType]int{apitype.OpType("same"): 2}).Equal(t, result.ChangeSummary)
}

func TestTranslateBasicWithEdit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	statePath := setupTFStack(t, "testdata/tf_random_stack", "tf_random_stack_cache")
	workspace := createPulumiStack(t)
	stackFolder := workspace.WorkDir()
	stackSummary, err := workspace.Stack(ctx)
	require.NoError(t, err)
	stackName := stackSummary.Name

	err = pkg.TranslateAndWriteStateWithWorkspace(statePath, workspace, filepath.Join(stackFolder, "state.json"))
	require.NoError(t, err)

	stack, err := auto.SelectStack(ctx, stackName, workspace)
	require.NoError(t, err)

	importStackState(t, ctx, stack, filepath.Join(stackFolder, "state.json"))

	// This changes the length of the random string from 16 to 17 in order to test that edits to resources still work.
	replaceIndexTs(t, stackFolder, filepath.Join("testdata/pulumi_random_stack2", "index.ts"))
	replacePackageJson(t, stackFolder, stackName, filepath.Join("testdata/pulumi_random_stack2", "package.json"))

	err = workspace.Install(ctx, nil)
	require.NoError(t, err)

	previewResult, err := stack.Preview(ctx, optpreview.Diff())
	require.NoError(t, err)
	autogold.Expect(map[apitype.OpType]int{
		apitype.OpType("replace"): 1,
		apitype.OpType("same"):    1,
	}).Equal(t, previewResult.ChangeSummary)

	upResult, err := stack.Up(ctx)
	require.NoError(t, err)

	autogold.Expect(&map[string]int{"replace": 1, "same": 1}).Equal(t, upResult.Summary.ResourceChanges)
}

func TestTranslateWithDependency(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	statePath := setupTFStack(t, "testdata/tf_dependency_stack", "tf_dependency_stack_cache")
	workspace := createPulumiStack(t)
	stackFolder := workspace.WorkDir()
	stackSummary, err := workspace.Stack(ctx)
	require.NoError(t, err)
	stackName := stackSummary.Name

	err = pkg.TranslateAndWriteStateWithWorkspace(statePath, workspace, filepath.Join(stackFolder, "state.json"))
	require.NoError(t, err)

	stack, err := auto.SelectStack(ctx, stackName, workspace)
	require.NoError(t, err)

	importStackState(t, ctx, stack, filepath.Join(stackFolder, "state.json"))

	replacePackageJson(t, stackFolder, stackName, filepath.Join("testdata/pulumi_dependency_stack", "package.json"))
	replaceIndexTs(t, stackFolder, filepath.Join("testdata/pulumi_dependency_stack", "index.ts"))

	err = workspace.Install(ctx, nil)
	require.NoError(t, err)

	upResult, err := stack.Up(ctx)
	require.NoError(t, err)

	autogold.Expect(&map[string]int{"same": 5}).Equal(t, upResult.Summary.ResourceChanges)
}

func TestTranslateAWSStack(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	statePath := setupTFStack(t, "testdata/tf_aws_stack", "tf_aws_stack_cache")
	workspace := createPulumiStack(t)
	stackFolder := workspace.WorkDir()
	stackSummary, err := workspace.Stack(ctx)
	require.NoError(t, err)
	stackName := stackSummary.Name

	err = pkg.TranslateAndWriteStateWithWorkspace(statePath, workspace, filepath.Join(stackFolder, "state.json"))
	require.NoError(t, err)

	stack, err := auto.SelectStack(ctx, stackName, workspace)
	require.NoError(t, err)

	importStackState(t, ctx, stack, filepath.Join(stackFolder, "state.json"))

	replacePackageJson(t, stackFolder, stackName, filepath.Join("testdata/pulumi_aws_stack", "package.json"))
	replaceIndexTs(t, stackFolder, filepath.Join("testdata/pulumi_aws_stack", "index.ts"))

	err = workspace.Install(ctx, nil)
	require.NoError(t, err)

	// TODO: Why do BucketLifecycleConfiguration and RolePolicy produce diffs.
	result, err := stack.Preview(ctx, optpreview.Diff())
	require.NoError(t, err)

	t.Logf("StdOut: %v", result.StdOut)
	t.Logf("StdErr: %v", result.StdErr)

	autogold.Expect(map[apitype.OpType]int{
		apitype.OpType("create"): 1,
		apitype.OpType("same"):   21,
		apitype.OpType("update"): 2,
	}).Equal(t, result.ChangeSummary)
}

// TODO if we skip the edit bits in this one is it still needed?
func TestTranslateAWSStackWithEdit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	statePath := setupTFStack(t, "testdata/tf_aws_stack", "tf_aws_stack_cache")
	workspace := createPulumiStack(t)
	stackFolder := workspace.WorkDir()
	stackSummary, err := workspace.Stack(ctx)
	require.NoError(t, err)
	stackName := stackSummary.Name

	err = pkg.TranslateAndWriteStateWithWorkspace(statePath, workspace, filepath.Join(stackFolder, "state.json"))
	require.NoError(t, err)

	stack, err := auto.SelectStack(ctx, stackName, workspace)
	require.NoError(t, err)

	importStackState(t, ctx, stack, filepath.Join(stackFolder, "state.json"))

	replacePackageJson(t, stackFolder, stackName, filepath.Join("testdata/pulumi_aws_stack", "package.json"))
	replaceIndexTs(t, stackFolder, filepath.Join("testdata/pulumi_aws_stack", "index.ts"))

	err = workspace.Install(ctx, nil)
	require.NoError(t, err)

	previewResult, err := stack.Preview(ctx, optpreview.Diff())

	t.Logf("StdOut: %v", previewResult.StdOut)
	t.Logf("StdErr: %v", previewResult.StdErr)

	require.NoError(t, err)

	autogold.Expect(map[apitype.OpType]int{
		apitype.OpType("create"): 1,
		apitype.OpType("same"):   21,
		apitype.OpType("update"): 2,
	}).Equal(t, previewResult.ChangeSummary)

	// pulumi up in AWS in CI is rather slow and expensive, can we skip this and limit to preview?

	// upResult, err := stack.Up(ctx)
	// require.NoError(t, err)

	// t.Logf("StdOut: %v", upResult.StdOut)
	// t.Logf("StdErr: %v", upResult.StdErr)

	// autogold.Expect(&map[string]int{"create": 1, "same": 21, "update": 2}).Equal(t, upResult.Summary.ResourceChanges)

	// replacePackageJson(t, stackFolder, stackName, filepath.Join("testdata/pulumi_aws_stack2", "package.json"))
	// replaceIndexTs(t, stackFolder, filepath.Join("testdata/pulumi_aws_stack2", "index.ts"))

	// upResult, err = stack.Up(ctx)

	// t.Logf("StdOut: %v", upResult.StdOut)
	// t.Logf("StdErr: %v", upResult.StdErr)

	// require.NoError(t, err)

	// autogold.Expect(&map[string]int{"same": 23, "update": 1}).Equal(t, upResult.Summary.ResourceChanges)
}
