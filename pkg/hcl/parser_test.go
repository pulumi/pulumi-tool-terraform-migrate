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

func TestParseModuleVariables(t *testing.T) {
	t.Parallel()
	vars, err := ParseModuleVariables("testdata/pet_module")
	require.NoError(t, err)
	require.Len(t, vars, 3)

	prefix := findVar(vars, "prefix")
	require.NotNil(t, prefix)
	require.Equal(t, "string", prefix.Type)
	require.Nil(t, prefix.Default)
	require.Equal(t, "Prefix for the pet name", prefix.Description)

	separator := findVar(vars, "separator")
	require.NotNil(t, separator)
	require.Equal(t, "string", separator.Type)
	require.NotNil(t, separator.Default)
	require.Equal(t, "-", separator.Default.AsString())

	length := findVar(vars, "length")
	require.NotNil(t, length)
	require.Equal(t, "number", length.Type)
	require.NotNil(t, length.Default)
}

func TestParseModuleOutputs(t *testing.T) {
	t.Parallel()
	outputs, err := ParseModuleOutputs("testdata/pet_module")
	require.NoError(t, err)
	require.Len(t, outputs, 2)

	name := findOutput(outputs, "name")
	require.NotNil(t, name)
	require.Equal(t, "The generated pet name", name.Description)
	require.NotNil(t, name.Expression)

	separator := findOutput(outputs, "separator")
	require.NotNil(t, separator)
	require.Empty(t, separator.Description)
	require.NotNil(t, separator.Expression)
}

func TestParseModuleVariables_NonexistentDir(t *testing.T) {
	t.Parallel()
	_, err := ParseModuleVariables("testdata/nonexistent")
	require.Error(t, err)
}

func TestParseModuleOutputs_NonexistentDir(t *testing.T) {
	t.Parallel()
	_, err := ParseModuleOutputs("testdata/nonexistent")
	require.Error(t, err)
}

func TestParseModuleCallSites(t *testing.T) {
	t.Parallel()
	calls, err := ParseModuleCallSites("testdata/root_with_pet")
	require.NoError(t, err)
	require.Len(t, calls, 2)

	pet := findCall(calls, "pet")
	require.NotNil(t, pet)
	require.Equal(t, "../pet_module", pet.Source)
	require.Len(t, pet.Arguments, 1) // prefix (count is not an argument)

	named := findCall(calls, "named_pet")
	require.NotNil(t, named)
	require.Equal(t, "../pet_module", named.Source)
	require.Len(t, named.Arguments, 3) // prefix, separator, length
}

func TestParseModuleCallSites_NonexistentDir(t *testing.T) {
	t.Parallel()
	_, err := ParseModuleCallSites("testdata/nonexistent")
	require.Error(t, err)
}

func TestLoadTfvars(t *testing.T) {
	t.Parallel()
	vars, err := LoadTfvars("testdata/root_with_pet/terraform.tfvars")
	require.NoError(t, err)
	require.Len(t, vars, 1)
	require.Equal(t, "production", vars["env"].AsString())
}

func TestLoadTfvars_NotFound(t *testing.T) {
	t.Parallel()
	vars, err := LoadTfvars("testdata/nonexistent/terraform.tfvars")
	require.NoError(t, err)
	require.Len(t, vars, 0)
}

func TestLoadAllTfvars(t *testing.T) {
	t.Parallel()
	vars, err := LoadAllTfvars("testdata/root_with_auto_tfvars")
	require.NoError(t, err)

	// From terraform.tfvars
	require.Equal(t, "prod", vars["env"].AsString())

	// From vpc.auto.tfvars
	require.Equal(t, "myvpc", vars["vpc_name"].AsString())
	require.Equal(t, "10.0.0.0/16", vars["vpc_cidr"].AsString())

	// From db.auto.tfvars
	require.Equal(t, "mydb", vars["db_name"].AsString())
}

func TestLoadAllTfvars_NoFiles(t *testing.T) {
	t.Parallel()
	vars, err := LoadAllTfvars("testdata/pet_module") // no tfvars files
	require.NoError(t, err)
	require.Len(t, vars, 0)
}

func TestParseLocals(t *testing.T) {
	t.Parallel()
	locals, err := ParseLocals("testdata/root_with_locals")
	require.NoError(t, err)
	require.Len(t, locals, 3) // name, upper_name, tags

	names := map[string]bool{}
	for _, l := range locals {
		names[l.Name] = true
		require.NotNil(t, l.Expression, "local %s should have an expression", l.Name)
	}
	require.True(t, names["name"])
	require.True(t, names["upper_name"])
	require.True(t, names["tags"])
}

func TestParseLocals_NoLocals(t *testing.T) {
	t.Parallel()
	// pet_module has no locals blocks
	locals, err := ParseLocals("testdata/pet_module")
	require.NoError(t, err)
	require.Len(t, locals, 0)
}

func TestParseResourceBlocks(t *testing.T) {
	t.Parallel()
	blocks, err := ParseResourceBlocks("testdata/pet_module")
	require.NoError(t, err)
	require.Len(t, blocks, 1)
	require.Equal(t, "random_pet", blocks[0].Type)
	require.Equal(t, "this", blocks[0].Name)
}

func TestParseResourceBlocks_MultipleTypes(t *testing.T) {
	// root_with_resource_ref has random_pet.base + module (no other resources in root)
	t.Parallel()
	blocks, err := ParseResourceBlocks("testdata/root_with_resource_ref")
	require.NoError(t, err)
	require.Len(t, blocks, 1)
	require.Equal(t, "random_pet", blocks[0].Type)
	require.Equal(t, "base", blocks[0].Name)
}

func TestParseResourceBlockAttrs(t *testing.T) {
	t.Parallel()
	// pet_module has: resource "random_pet" "this" { prefix, separator, length }
	attrs, err := ParseResourceBlockAttrs("testdata/pet_module", "random_pet", "this")
	require.NoError(t, err)
	require.Contains(t, attrs, "prefix")
	require.Contains(t, attrs, "separator")
	require.Contains(t, attrs, "length")
}

func TestParseResourceBlockAttrs_NotFound(t *testing.T) {
	t.Parallel()
	attrs, err := ParseResourceBlockAttrs("testdata/pet_module", "aws_vpc", "this")
	require.NoError(t, err)
	require.Empty(t, attrs)
}

func findCall(calls []ModuleCallSite, name string) *ModuleCallSite {
	for i := range calls {
		if calls[i].Name == name {
			return &calls[i]
		}
	}
	return nil
}

func findVar(vars []ModuleVariable, name string) *ModuleVariable {
	for i := range vars {
		if vars[i].Name == name {
			return &vars[i]
		}
	}
	return nil
}

func findOutput(outputs []ModuleOutput, name string) *ModuleOutput {
	for i := range outputs {
		if outputs[i].Name == name {
			return &outputs[i]
		}
	}
	return nil
}
