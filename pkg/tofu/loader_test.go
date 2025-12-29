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
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_LoadTerraformState(t *testing.T) {
	t.Parallel()
	skipIfTofuNotAvailable(t)

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
			expectResources:          1,
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

// skipIfTofuNotAvailable skips the test if tofu is not in PATH
func skipIfTofuNotAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skip("tofu binary not found in PATH, skipping test")
	}
}
