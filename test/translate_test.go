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

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hexops/autogold/v2"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
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
	return dir
}

func createPulumiStack(t *testing.T) (string, string) {
	dir, err := os.MkdirTemp("", "pulumi-stack-")
	require.NoError(t, err)
	t.Logf("Pulumi stack directory: %s", dir)

	stackName := filepath.Base(dir)

	_ = runCommand(t, dir, "pulumi", "new", "typescript", "--dir", dir, "--yes", "--stack", stackName)
	_ = runCommand(t, dir, "pulumi", "up", "--yes")

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
	isolatePulumiHome(t)

	statePath := setupTFStack(t, "testdata/tf_random_stack")
	stackFolder, stackName := createPulumiStack(t)

	ctx := context.Background()

	err = pkg.TranslateAndWriteState(ctx, statePath, stackFolder, filepath.Join(stackFolder, "state.json"), "")
	require.NoError(t, err)

	_ = runCommand(t, stackFolder, "pulumi", "stack", "import", "--file", filepath.Join(stackFolder, "state.json"))

	replaceIndexTs(t, stackFolder, filepath.Join("testdata/pulumi_random_stack", "index.ts"))
	replacePackageJson(t, stackFolder, stackName, filepath.Join("testdata/pulumi_random_stack", "package.json"))

	workspace, err := auto.NewLocalWorkspace(ctx, auto.WorkDir(stackFolder))
	require.NoError(t, err)

	err = workspace.Install(ctx, nil)
	require.NoError(t, err)

	stack, err := auto.SelectStack(ctx, stackName, workspace)
	require.NoError(t, err)
	require.NotNil(t, stack)

	result, err := stack.Preview(ctx, optpreview.Diff())
	require.NoError(t, err)

	t.Logf("pulumi preview --diff:\n%s\n%s", result.StdOut, result.StdErr)

	autogold.Expect(map[apitype.OpType]int{apitype.OpType("same"): 2}).Equal(t, result.ChangeSummary)
}

func TestTranslateBasicWithDependencies(t *testing.T) {
	isolatePulumiHome(t)

	ctx := context.Background()

	statePath := setupTFStack(t, "testdata/tf_random_stack")
	stackFolder, _ := createPulumiStack(t)

	err := pkg.TranslateAndWriteState(ctx, statePath, stackFolder, filepath.Join(stackFolder, "state.json"), filepath.Join(stackFolder, "dependencies.json"))
	require.NoError(t, err)

	dependencies, err := os.ReadFile(filepath.Join(stackFolder, "dependencies.json"))
	require.NoError(t, err)
	autogold.Expect(`[{"name":"random","version":"4.18.1"}]`).Equal(t, string(dependencies))
}

func TestTranslateBasicWithEdit(t *testing.T) {
	isolatePulumiHome(t)

	ctx := context.Background()

	statePath := setupTFStack(t, "testdata/tf_random_stack")
	stackFolder, stackName := createPulumiStack(t)

	err := pkg.TranslateAndWriteState(ctx, statePath, stackFolder, filepath.Join(stackFolder, "state.json"), "")
	require.NoError(t, err)

	_ = runCommand(t, stackFolder, "pulumi", "stack", "import", "--file", filepath.Join(stackFolder, "state.json"))

	// This changes the length of the random string from 16 to 17 in order to test that edits to resources still work.
	replaceIndexTs(t, stackFolder, filepath.Join("testdata/pulumi_random_stack2", "index.ts"))
	replacePackageJson(t, stackFolder, stackName, filepath.Join("testdata/pulumi_random_stack2", "package.json"))

	workspace, err := auto.NewLocalWorkspace(ctx, auto.WorkDir(stackFolder))
	require.NoError(t, err)

	err = workspace.Install(ctx, nil)
	require.NoError(t, err)

	stack, err := auto.SelectStack(ctx, stackName, workspace)
	require.NoError(t, err)
	require.NotNil(t, stack)

	result, err := stack.Preview(ctx, optpreview.Diff())
	require.NoError(t, err)

	t.Logf("pulumi preview --diff:\n%s\n%s", result.StdOut, result.StdErr)

	autogold.Expect(map[apitype.OpType]int{
		apitype.OpType("replace"): 1,
		apitype.OpType("same"):    1,
	}).Equal(t, result.ChangeSummary)

	upResult, err := stack.Up(ctx)

	// TODO[pulumi/pulumi-random#1967] intermittent failures here.
	if strings.Contains(upResult.StdErr+upResult.StdOut, "string field contains invalid UTF-8") {
		return
	}

	require.NoError(t, err)

	t.Logf("pulumi up:\n%s\n%s", upResult.StdOut, upResult.StdErr)
	autogold.Expect(map[apitype.OpType]int{
		apitype.OpType("replace"): 1,
		apitype.OpType("same"):    1,
	}).Equal(t, result.ChangeSummary)

	autogold.Expect(map[apitype.OpType]int{
		apitype.OpType("replace"): 1,
		apitype.OpType("same"):    1,
	}).Equal(t, result.ChangeSummary)
}

func TestTranslateWithDependency(t *testing.T) {
	t.Parallel()
	skipIfCI(t)

	ctx := context.Background()

	statePath := setupTFStack(t, "testdata/tf_dependency_stack")
	stackFolder, stackName := createPulumiStack(t)

	err := pkg.TranslateAndWriteState(ctx, statePath, stackFolder, filepath.Join(stackFolder, "state.json"), "")
	require.NoError(t, err)

	_ = runCommand(t, stackFolder, "pulumi", "stack", "import", "--file", filepath.Join(stackFolder, "state.json"))

	replacePackageJson(t, stackFolder, stackName, filepath.Join("testdata/pulumi_dependency_stack", "package.json"))
	replaceIndexTs(t, stackFolder, filepath.Join("testdata/pulumi_dependency_stack", "index.ts"))
	_ = runCommand(t, stackFolder, "pulumi", "install")

	output := runCommand(t, stackFolder, "pulumi", "up", "--yes")
	autogold.ExpectFile(t, output)
}

func TestTranslateAWSStack(t *testing.T) {
	t.Parallel()
	skipIfCI(t)

	ctx := context.Background()

	statePath := setupTFStack(t, "testdata/tf_aws_stack")
	stackFolder, stackName := createPulumiStack(t)

	err := pkg.TranslateAndWriteState(ctx, statePath, stackFolder, filepath.Join(stackFolder, "state.json"), "")
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
	t.Parallel()
	skipIfCI(t)

	ctx := context.Background()

	statePath := setupTFStack(t, "testdata/tf_aws_stack")
	stackFolder, stackName := createPulumiStack(t)

	err := pkg.TranslateAndWriteState(ctx, statePath, stackFolder, filepath.Join(stackFolder, "state.json"), "")
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

// Running many tests in parallel exposes race conditions in Pulumi plugin handling; isolate PULUMI_HOME. Note that
// this helper conflicts with `t.Parallel()`.
func isolatePulumiHome(t *testing.T) {
	t.Helper()
	pulumiHome, err := os.MkdirTemp("", "pulumi-home-")
	require.NoError(t, err)
	t.Cleanup(func() {
		os.RemoveAll(pulumiHome)
	})
	t.Setenv("PULUMI_HOME", pulumiHome)
}
