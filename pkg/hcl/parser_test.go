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
