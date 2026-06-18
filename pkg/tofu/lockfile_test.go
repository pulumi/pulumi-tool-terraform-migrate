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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLockfileVersions(t *testing.T) {
	t.Parallel()

	content := `
provider "registry.terraform.io/hashicorp/aws" {
  version     = "5.31.0"
  constraints = ">= 5.0.0"
  hashes = [
    "h1:abc123=",
  ]
}

provider "registry.terraform.io/hashicorp/random" {
  version     = "3.7.2"
  constraints = "~> 3.0"
  hashes = [
    "h1:xyz456=",
  ]
}
`
	versions := parseLockfileVersions(content)

	require.Len(t, versions, 2)
	assert.Equal(t, "5.31.0", versions["registry.terraform.io/hashicorp/aws"])
	assert.Equal(t, "3.7.2", versions["registry.terraform.io/hashicorp/random"])
}

func TestParseLockfileVersions_Empty(t *testing.T) {
	t.Parallel()
	versions := parseLockfileVersions("")
	assert.Empty(t, versions)
}

func TestGetProviderVersionsFromLockfile_Exists(t *testing.T) {
	t.Parallel()

	versions, err := GetProviderVersionsFromLockfile("testdata/tf-project-with-lockfile")
	require.NoError(t, err)
	require.Contains(t, versions, "registry.terraform.io/hashicorp/random")
	assert.Equal(t, "3.7.2", versions["registry.terraform.io/hashicorp/random"])
}

func TestGetProviderVersionsFromLockfile_Missing(t *testing.T) {
	t.Parallel()

	// Directory without a lockfile should return empty map, not error.
	versions, err := GetProviderVersionsFromLockfile("testdata/tf-project")
	require.NoError(t, err)
	assert.Empty(t, versions)
}
