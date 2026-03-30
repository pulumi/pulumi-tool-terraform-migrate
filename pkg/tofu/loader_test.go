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
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_LoadTerraformState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	type testCase struct {
		name            string
		opts            LoadTerraformStateOptions
		expectResources int
		// Optional: verify lockfile is preserved after LoadTerraformState
		verifyLockfilePreserved bool
	}

	testCases := []testCase{
		{
			name: "tofu-state-filepath",
			opts: LoadTerraformStateOptions{
				StateFilePath: "testdata/tofu-project/terraform.tfstate",
			},
			expectResources: 1,
		},
		{
			name: "tofu-state-projectdir",
			opts: LoadTerraformStateOptions{
				ProjectDir: "testdata/tofu-project",
			},
			expectResources: 1,
		},
		{
			name: "tf-state-filepath",
			opts: LoadTerraformStateOptions{
				StateFilePath: "testdata/tf-project/terraform.tfstate",
			},
			expectResources: 1,
		},
		{
			name: "tf-state-with-lockfile-preservation",
			opts: LoadTerraformStateOptions{
				StateFilePath: "testdata/tf-project-with-lockfile/terraform.tfstate",
			},
			expectResources:         1,
			verifyLockfilePreserved: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var lockfileContentBefore []byte
			var lockfilePath string

			// Pre-processing: read lockfile if verification is requested
			if tc.verifyLockfilePreserved {
				projectDir := tc.opts.ProjectDir
				if projectDir == "" && tc.opts.StateFilePath != "" {
					projectDir = filepath.Dir(tc.opts.StateFilePath)
				}
				lockfilePath = filepath.Join(projectDir, ".terraform.lock.hcl")

				var err error
				lockfileContentBefore, err = os.ReadFile(lockfilePath)
				require.NoError(t, err, "failed to read lockfile before test")
				require.NotEmpty(t, lockfileContentBefore, "lockfile should exist before test")
			}

			state, err := LoadTerraformState(ctx, tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.expectResources, len(state.Values.RootModule.Resources))

			// Post-processing: verify lockfile was preserved
			if tc.verifyLockfilePreserved {
				lockfileContentAfter, err := os.ReadFile(lockfilePath)
				require.NoError(t, err, "failed to read lockfile after test")
				require.Equal(t, lockfileContentBefore, lockfileContentAfter,
					"lockfile should be preserved after LoadTerraformState")
			}
		})
	}
}

// copyTestdata copies a testdata directory to a temp dir to avoid .terraform side effects in the source tree.
func copyTestdata(t *testing.T, srcDir string) string {
	t.Helper()
	tmpDir := t.TempDir()
	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(tmpDir, relPath)
		if d.IsDir() {
			return os.MkdirAll(destPath, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, data, 0o644)
	})
	require.NoError(t, err)
	return tmpDir
}

func Test_LoadTerraformState_SingleWorkspace_StateFilePath(t *testing.T) {
	token := os.Getenv("PULUMI_ACCESS_TOKEN")
	if token == "" {
		t.Skip("PULUMI_ACCESS_TOKEN not set")
	}
	t.Setenv("TF_TOKEN_api_pulumi_com", token)

	// Copy testdata to temp dir to avoid .terraform side effects
	dir := copyTestdata(t, "testdata/tf-single-workspace")

	// Borrow the Terraform-generated state from tf-project (which uses registry.terraform.io
	// providers) and write it to a separate temp dir so tofu init doesn't trigger a state
	// migration prompt. This exercises the provider-rewrite path for explicit state files on
	// a backend that only supports a single named workspace.
	stateBytes, err := os.ReadFile("testdata/tf-project/terraform.tfstate")
	require.NoError(t, err)
	stateFile := filepath.Join(t.TempDir(), "terraform.tfstate")
	require.NoError(t, os.WriteFile(stateFile, stateBytes, 0o600))

	ctx := context.Background()
	state, err := LoadTerraformState(ctx, LoadTerraformStateOptions{
		StateFilePath: stateFile,
		ProjectDir:    dir,
	})
	require.NoError(t, err)
	require.Equal(t, 1, len(state.Values.RootModule.Resources))
}

func Test_LoadTerraformState_SingleWorkspace_ProjectDir(t *testing.T) {
	token := os.Getenv("PULUMI_ACCESS_TOKEN")
	if token == "" {
		t.Skip("PULUMI_ACCESS_TOKEN not set")
	}
	t.Setenv("TF_TOKEN_api_pulumi_com", token)

	dir := copyTestdata(t, "testdata/tf-single-workspace")

	ctx := context.Background()
	state, err := LoadTerraformState(ctx, LoadTerraformStateOptions{
		ProjectDir: dir,
	})
	require.NoError(t, err)
	require.NotNil(t, state)
}

// Test_parseStateWithProviderRewrite verifies the registry rewrite logic that is the core of the
// loadWorkspaceState fallback path. This exercises parseStateWithProviderRewrite directly, which
// is shared by both loadStateWithRewrite (backend pull) and loadStateFileWithRewrite (local file).
func Test_parseStateWithProviderRewrite(t *testing.T) {
	// Read a Terraform-generated state file that uses registry.terraform.io provider references.
	stateData, err := os.ReadFile("testdata/tf-project/terraform.tfstate")
	require.NoError(t, err)

	// Verify the state contains registry.terraform.io references before rewrite.
	require.Contains(t, string(stateData), "registry.terraform.io/")

	// We need a tofu instance with providers initialized. Use copyTestdata to get an isolated
	// copy, then run tofu init (which downloads provider binaries with correct permissions).
	dir := copyTestdata(t, "testdata/tf-project")
	tofu, err := tofuNew(dir)
	require.NoError(t, err)

	// Remove any stale .terraform that copyTestdata may have brought over (provider binaries
	// lose execute permission when copied with 0o644), so tofu init downloads fresh.
	require.NoError(t, os.RemoveAll(filepath.Join(dir, ".terraform")))
	require.NoError(t, tofu.Init(context.Background()))

	state, err := parseStateWithProviderRewrite(context.Background(), tofu, stateData)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Equal(t, 1, len(state.Values.RootModule.Resources))

	// Verify provider address was rewritten in the parsed state.
	res := state.Values.RootModule.Resources[0]
	require.Contains(t, res.ProviderName, "registry.opentofu.org")
	require.NotContains(t, res.ProviderName, "registry.terraform.io")
}

func Test_GetProviderVersions(t *testing.T) {

	ctx := context.Background()
	projectDir := "testdata/tf-project-with-lockfile"

	versionOutput, err := GetProviderVersions(ctx, projectDir)
	require.NoError(t, err, "GetProviderVersions should not fail")

	require.NotNil(t, versionOutput.ProviderSelections, "ProviderSelections should not be nil")
	require.Equal(t, 1, len(versionOutput.ProviderSelections), "Expected 1 provider")

	version, exists := versionOutput.ProviderSelections["registry.terraform.io/hashicorp/random"]
	require.True(t, exists, "Expected registry.terraform.io/hashicorp/random to exist in ProviderSelections")
	require.Equal(t, "3.7.2", version, "Expected provider version to match lock file")

	require.NotEmpty(t, versionOutput.TerraformVersion, "TerraformVersion should be populated")
	require.NotEmpty(t, versionOutput.Platform, "Platform should be populated")
}
