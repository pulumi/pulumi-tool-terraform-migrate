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
	"os"
	"testing"

	"github.com/hexops/autogold/v2"
	"github.com/stretchr/testify/require"
)

func createPulumiStack(t *testing.T) string {
	dir, err := os.MkdirTemp("", "pulumi-stack-")
	require.NoError(t, err)
	t.Logf("Pulumi stack directory: %s", dir)

	_ = runCommand(t, dir, "pulumi", "new", "typescript", "--dir", dir, "--yes")
	_ = runCommand(t, dir, "pulumi", "up", "--yes")
	return dir
}

func TestConvertSimple(t *testing.T) {
	t.Parallel()
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping test in CI: TODO: set up pulumi credentials in CI")
	}
	stackFolder := createPulumiStack(t)
	data, err := TranslateState("testdata/bucket_state.json", stackFolder)
	if err != nil {
		t.Fatalf("failed to convert Terraform state: %v", err)
	}

	autogold.ExpectFile(t, data.Export)
}

func TestConvertWithDependencies(t *testing.T) {
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping test in CI: TODO: set up pulumi credentials in CI")
	}
	stackFolder := createPulumiStack(t)
	res, err := TranslateState("testdata/bucket_state.json", stackFolder)
	if err != nil {
		t.Fatalf("failed to convert Terraform state: %v", err)
	}

	require.Equal(t, 1, len(res.RequiredProviders))
	require.Equal(t, "aws", res.RequiredProviders[0].Name)
	require.Equal(t, "7.12.0", res.RequiredProviders[0].Version)
}

func TestConvertInvolved(t *testing.T) {
	t.Parallel()
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping test in CI: TODO: set up pulumi credentials in CI")
	}
	stackFolder := createPulumiStack(t)
	data, err := TranslateState("testdata/tofu_state.json", stackFolder)
	if err != nil {
		t.Fatalf("failed to convert Terraform state: %v", err)
	}

	autogold.ExpectFile(t, data)
}
