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
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func loadTofuShowJSON(t *testing.T, path string) *tfjson.State {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var state tfjson.State
	require.NoError(t, json.Unmarshal(data, &state))
	return &state
}

func TestBuildModuleMap_WithoutEval(t *testing.T) {
	tfDir, err := filepath.Abs(filepath.Join("testdata", "tf_indexed_modules"))
	require.NoError(t, err)

	config, err := LoadConfig(tfDir)
	require.NoError(t, err)

	tfjsonState := loadTofuShowJSON(t, filepath.Join("testdata", "tofu_state_indexed_modules.json"))

	// Build without eval (nil tofuCtx, nil state) — no pulumiProviders needed for URN
	// generation in this test since we just check structure.
	mm, err := BuildModuleMap(config, nil, nil, tfjsonState, nil, "test-stack", "test-project")
	require.NoError(t, err)
	require.NotNil(t, mm)

	// Should have "pet[0]" and "pet[1]" entries.
	require.Contains(t, mm.Modules, "pet[0]")
	require.Contains(t, mm.Modules, "pet[1]")

	pet0 := mm.Modules["pet[0]"]
	assert.Equal(t, "module.pet[0]", pet0.TerraformPath)
	assert.Equal(t, "./modules/pet", pet0.Source)
	assert.Equal(t, "0", pet0.IndexKey)
	assert.Equal(t, "int", pet0.IndexType)

	pet1 := mm.Modules["pet[1]"]
	assert.Equal(t, "module.pet[1]", pet1.TerraformPath)
	assert.Equal(t, "1", pet1.IndexKey)

	// Resources should be populated (without provider mapping, URNs will be raw addresses).
	assert.Len(t, pet0.Resources, 1)
	assert.Len(t, pet1.Resources, 1)

	// Interface should be populated from config.
	require.NotNil(t, pet0.Interface)
	require.Len(t, pet0.Interface.Inputs, 1)
	assert.Equal(t, "prefix", pet0.Interface.Inputs[0].Name)
	assert.True(t, pet0.Interface.Inputs[0].Required)
	require.Len(t, pet0.Interface.Outputs, 1)
	assert.Equal(t, "name", pet0.Interface.Outputs[0].Name)

	// Without eval, evaluatedValue should be nil.
	assert.Nil(t, pet0.Interface.Inputs[0].EvaluatedValue)
}

func TestBuildModuleMap_WithEval(t *testing.T) {
	tfDir, err := filepath.Abs(filepath.Join("testdata", "tf_indexed_modules"))
	require.NoError(t, err)

	providerDir := filepath.Join(tfDir, ".terraform", "providers")
	if _, err := os.Stat(providerDir); os.IsNotExist(err) {
		t.Skip("skipping: .terraform/providers not found")
	}

	config, err := LoadConfig(tfDir)
	require.NoError(t, err)

	rawState, err := LoadRawState(filepath.Join(tfDir, "terraform.tfstate"))
	require.NoError(t, err)

	tfjsonState := loadTofuShowJSON(t, filepath.Join("testdata", "tofu_state_indexed_modules.json"))

	tofuCtx, err := Evaluate(config, rawState, tfDir)
	require.NoError(t, err)

	mm, err := BuildModuleMap(config, tofuCtx, rawState, tfjsonState, nil, "test-stack", "test-project")
	require.NoError(t, err)
	require.NotNil(t, mm)

	pet0 := mm.Modules["pet[0]"]
	require.NotNil(t, pet0)
	require.NotNil(t, pet0.Interface)
	require.Len(t, pet0.Interface.Inputs, 1)

	// With eval, evaluatedValue for "prefix" in pet[0] should be "test-0".
	assert.Equal(t, "test-0", pet0.Interface.Inputs[0].EvaluatedValue)

	pet1 := mm.Modules["pet[1]"]
	require.NotNil(t, pet1)
	require.NotNil(t, pet1.Interface)
	assert.Equal(t, "test-1", pet1.Interface.Inputs[0].EvaluatedValue)
}

func TestBuildModuleMap_Expression(t *testing.T) {
	tfDir, err := filepath.Abs(filepath.Join("testdata", "tf_indexed_modules"))
	require.NoError(t, err)

	config, err := LoadConfig(tfDir)
	require.NoError(t, err)

	tfjsonState := loadTofuShowJSON(t, filepath.Join("testdata", "tofu_state_indexed_modules.json"))

	mm, err := BuildModuleMap(config, nil, nil, tfjsonState, nil, "test-stack", "test-project")
	require.NoError(t, err)

	pet0 := mm.Modules["pet[0]"]
	require.NotNil(t, pet0)
	require.NotNil(t, pet0.Interface)
	require.Len(t, pet0.Interface.Inputs, 1)

	// The expression for "prefix" should be the call-site expression text.
	assert.Contains(t, pet0.Interface.Inputs[0].Expression, "test-${count.index}")
}

func TestWriteModuleMap(t *testing.T) {
	mm := &ModuleMap{
		Modules: map[string]*ModuleMapEntry{
			"vpc": {
				TerraformPath: "module.vpc",
				Source:        "./modules/vpc",
				Resources:     []string{"urn:pulumi:stack::project::aws:ec2/vpc:Vpc::main"},
				Interface: &ModuleInterface{
					Inputs:  []ModuleInterfaceField{{Name: "cidr", Required: true}},
					Outputs: []ModuleInterfaceField{{Name: "id"}},
				},
			},
		},
	}

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "module-map.json")

	err := WriteModuleMap(mm, outPath)
	require.NoError(t, err)

	// Read back and verify round-trip.
	data, err := os.ReadFile(outPath)
	require.NoError(t, err)

	var got ModuleMap
	require.NoError(t, json.Unmarshal(data, &got))

	require.Contains(t, got.Modules, "vpc")
	assert.Equal(t, "module.vpc", got.Modules["vpc"].TerraformPath)
	assert.Equal(t, "./modules/vpc", got.Modules["vpc"].Source)
	assert.Len(t, got.Modules["vpc"].Resources, 1)
	require.NotNil(t, got.Modules["vpc"].Interface)
	assert.Len(t, got.Modules["vpc"].Interface.Inputs, 1)
	assert.Equal(t, "cidr", got.Modules["vpc"].Interface.Inputs[0].Name)
}

func TestCtyValueToInterface(t *testing.T) {
	tests := []struct {
		name     string
		input    cty.Value
		expected interface{}
	}{
		{"null", cty.NilVal, nil},
		{"string", cty.StringVal("hello"), "hello"},
		{"int_number", cty.NumberIntVal(42), int64(42)},
		{"float_number", cty.NumberFloatVal(3.14), 3.14},
		{"bool_true", cty.True, true},
		{"bool_false", cty.False, false},
		{"unknown", cty.UnknownVal(cty.String), nil},
		{
			"list",
			cty.ListVal([]cty.Value{cty.StringVal("a"), cty.StringVal("b")}),
			[]interface{}{"a", "b"},
		},
		{
			"map",
			cty.MapVal(map[string]cty.Value{"k": cty.StringVal("v")}),
			map[string]interface{}{"k": "v"},
		},
		{
			"object",
			cty.ObjectVal(map[string]cty.Value{
				"name": cty.StringVal("test"),
				"num":  cty.NumberIntVal(1),
			}),
			map[string]interface{}{"name": "test", "num": int64(1)},
		},
		{
			"tuple",
			cty.TupleVal([]cty.Value{cty.StringVal("x"), cty.NumberIntVal(2)}),
			[]interface{}{"x", int64(2)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ctyValueToInterface(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
