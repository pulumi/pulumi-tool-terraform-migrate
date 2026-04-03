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

func TestResolveModuleSourcesFromCache(t *testing.T) {
	t.Parallel()
	sources, err := ResolveModuleSourcesFromCache("testdata/root_with_module_cache")
	require.NoError(t, err)

	// "pet" -> "module.pet"
	petDir, ok := sources["module.pet"]
	require.True(t, ok, "should resolve module.pet from cache")
	require.Contains(t, petDir, ".terraform/modules/pet")

	// "nested.child" -> "module.nested.module.child"
	childDir, ok := sources["module.nested.module.child"]
	require.True(t, ok, "should resolve module.nested.module.child from cache")
	require.Contains(t, childDir, ".terraform/modules/nested/modules/child")

	// Root module entry should be excluded
	_, hasRoot := sources[""]
	require.False(t, hasRoot)
}

func TestResolveModuleSourcesFromCache_NoCacheDir(t *testing.T) {
	t.Parallel()
	sources, err := ResolveModuleSourcesFromCache("testdata/root_with_pet")
	require.NoError(t, err)
	require.Len(t, sources, 0)
}

func TestManifestKeyToModuleAddr(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"vpc", "module.vpc"},
		{"rdsdb.db_subnet_group", "module.rdsdb.module.db_subnet_group"},
		{"a.b.c", "module.a.module.b.module.c"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			require.Equal(t, tt.expected, manifestKeyToModuleAddr(tt.key))
		})
	}
}
