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
