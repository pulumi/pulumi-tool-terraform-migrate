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

package hcl

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiscoverModuleSources_LocalPaths(t *testing.T) {
	t.Parallel()
	sources, err := DiscoverModuleSources("testdata/root_with_pet")
	require.NoError(t, err)
	require.Equal(t, "../pet_module", sources["module.pet"])
	require.Equal(t, "../pet_module", sources["module.named_pet"])
}

func TestDiscoverModuleSources_RegistrySkipped(t *testing.T) {
	t.Parallel()
	sources, err := DiscoverModuleSources("testdata/root_with_registry")
	require.NoError(t, err)

	// Registry source should be skipped
	_, ok := sources["module.vpc"]
	require.False(t, ok, "registry modules should not be auto-discovered")

	// Local source should be found
	require.Equal(t, "./modules/pet", sources["module.local_pet"])
}

func TestDiscoverModuleSources_NonexistentDir(t *testing.T) {
	t.Parallel()
	_, err := DiscoverModuleSources("testdata/does_not_exist")
	require.Error(t, err)
}
