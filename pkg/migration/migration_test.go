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

package migration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadMigrationWithModules(t *testing.T) {
	mf, err := LoadMigration("testdata/migration_with_modules.json")
	require.NoError(t, err)
	require.Len(t, mf.Migration.Stacks[0].Modules, 2)

	vpc := mf.Migration.Stacks[0].Modules[0]
	require.Equal(t, "module.vpc", vpc.TFModule)
	require.Equal(t, "myproject:index:VpcComponent", vpc.PulumiType)
	require.Equal(t, "./schemas/vpc-component.json", vpc.SchemaPath)
	require.Empty(t, vpc.HCLSource)

	subnets := mf.Migration.Stacks[0].Modules[1]
	require.Equal(t, "module.vpc.module.subnets", subnets.TFModule)
	require.Equal(t, "myproject:network:SubnetGroup", subnets.PulumiType)
	require.Equal(t, "./modules/subnets", subnets.HCLSource)
}

func TestLoadMigrationWithoutModules_BackwardCompatible(t *testing.T) {
	mf, err := LoadMigration("testdata/migration_no_modules.json")
	require.NoError(t, err)
	require.Nil(t, mf.Migration.Stacks[0].Modules)
}
