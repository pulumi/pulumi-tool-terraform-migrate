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
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func TestPopulateComponentsFromHCL_VariableDefaults(t *testing.T) {
	// The "named_pet" call site passes prefix, separator, length — all explicit, no defaults needed.
	// The "pet" call site passes only prefix — separator and length should get defaults.
	components := []PulumiResource{
		{PulumiResourceID: PulumiResourceID{Name: "named-pet", Type: "terraform:module/namedPet:NamedPet"}},
	}
	tree := []*componentNode{
		{name: "named_pet", resourceName: "named-pet", typeToken: "terraform:module/namedPet:NamedPet"},
	}

	metadata, err := populateComponentsFromHCL(components, tree, nil, nil, "hcl/testdata/root_with_pet", true, nil)
	require.NoError(t, err)
	require.Nil(t, metadata) // metadata is nil when populateInputs=true

	// named_pet passes all three args explicitly — no defaults needed
	inputs := components[0].Inputs
	require.NotNil(t, inputs)
	require.Contains(t, inputs, resource.PropertyKey("prefix"))
	require.Contains(t, inputs, resource.PropertyKey("separator"))
	require.Contains(t, inputs, resource.PropertyKey("length"))

	// separator was explicitly passed as "_"
	require.Equal(t, resource.NewStringProperty("_"), inputs["separator"])
	// length was explicitly passed as 3
	require.Equal(t, resource.NewNumberProperty(3), inputs["length"])
}

func TestPopulateComponentsFromHCL_VariableDefaultsMerged(t *testing.T) {
	// Use "pet" module call which only passes prefix — defaults for separator and length should merge.
	// pet has count=2 so we test with pet-0 instance.
	components := []PulumiResource{
		{PulumiResourceID: PulumiResourceID{Name: "pet-0", Type: "terraform:module/pet:Pet"}},
	}
	tree := []*componentNode{
		{name: "pet", key: "0", resourceName: "pet-0", typeToken: "terraform:module/pet:Pet"},
	}

	metadata, err := populateComponentsFromHCL(components, tree, nil, nil, "hcl/testdata/root_with_pet", true, nil)
	require.NoError(t, err)
	require.Nil(t, metadata)

	inputs := components[0].Inputs
	require.NotNil(t, inputs)

	// prefix was passed as "test-${count.index}" → evaluated with count.index=0 → "test-0"
	require.Contains(t, inputs, resource.PropertyKey("prefix"))
	require.Equal(t, resource.NewStringProperty("test-0"), inputs["prefix"])

	// separator was NOT in call site, default is "-"
	require.Contains(t, inputs, resource.PropertyKey("separator"))
	require.Equal(t, resource.NewStringProperty("-"), inputs["separator"])

	// length was NOT in call site, default is 2
	require.Contains(t, inputs, resource.PropertyKey("length"))
	require.Equal(t, resource.NewNumberProperty(2), inputs["length"])
}

func TestPopulateComponentsFromHCL_NoInputsWhenFlagFalse(t *testing.T) {
	// When populateInputs=false, component inputs should be empty
	// and metadata should be returned.
	components := []PulumiResource{
		{PulumiResourceID: PulumiResourceID{Name: "named-pet", Type: "terraform:module/namedPet:NamedPet"}},
	}
	tree := []*componentNode{
		{name: "named_pet", resourceName: "named-pet", typeToken: "terraform:module/namedPet:NamedPet"},
	}

	metadata, err := populateComponentsFromHCL(components, tree, nil, nil, "hcl/testdata/root_with_pet", false, nil)
	require.NoError(t, err)

	// Inputs should be empty (not populated)
	require.Nil(t, components[0].Inputs)

	// Metadata should be returned
	require.NotNil(t, metadata)
	schema, ok := metadata.Components["module.named_pet"]
	require.True(t, ok)
	require.Len(t, schema.Inputs, 3) // prefix, separator, length
	require.Len(t, schema.Outputs, 2) // name, separator
}

func TestPopulateComponentsFromHCL_ResourceAttrRef(t *testing.T) {
	// When resource attrs are passed, call-site expressions that reference
	// resource attributes (e.g., random_pet.base.id) should resolve.
	components := []PulumiResource{
		{PulumiResourceID: PulumiResourceID{Name: "consumer", Type: "terraform:module/consumer:Consumer"}},
	}
	tree := []*componentNode{
		{name: "consumer", resourceName: "consumer", typeToken: "terraform:module/consumer:Consumer"},
	}

	// Simulate TF state with a random_pet.base resource
	resourceAttrs := map[string]map[string]cty.Value{
		"random_pet": {
			"base": cty.ObjectVal(map[string]cty.Value{
				"id":        cty.StringVal("base-happy-fox"),
				"prefix":    cty.StringVal("base"),
				"separator": cty.StringVal("-"),
			}),
		},
	}

	metadata, err := populateComponentsFromHCL(components, tree, nil, nil, "hcl/testdata/root_with_resource_ref", true, resourceAttrs)
	require.NoError(t, err)
	require.Nil(t, metadata)

	inputs := components[0].Inputs
	require.NotNil(t, inputs)

	// prefix = random_pet.base.id → should resolve to "base-happy-fox"
	require.Contains(t, inputs, resource.PropertyKey("prefix"))
	require.Equal(t, resource.NewStringProperty("base-happy-fox"), inputs["prefix"])
}

func TestPopulateComponentsFromHCL_ResourceAttrRef_NilAttrs(t *testing.T) {
	// When no resource attrs are passed (nil), expressions referencing
	// resource attributes should fail gracefully (warning, not error).
	components := []PulumiResource{
		{PulumiResourceID: PulumiResourceID{Name: "consumer", Type: "terraform:module/consumer:Consumer"}},
	}
	tree := []*componentNode{
		{name: "consumer", resourceName: "consumer", typeToken: "terraform:module/consumer:Consumer"},
	}

	metadata, err := populateComponentsFromHCL(components, tree, nil, nil, "hcl/testdata/root_with_resource_ref", true, nil)
	require.NoError(t, err) // Should not error — just warn and skip unresolvable inputs
	require.Nil(t, metadata)

	inputs := components[0].Inputs
	// prefix = random_pet.base.id can't resolve without resource attrs
	// separator and length should still be present (literal value and default)
	if inputs != nil {
		require.NotContains(t, inputs, resource.PropertyKey("prefix")) // skipped due to eval failure
	}
}

func TestPopulateComponentsFromHCL_OutputNames(t *testing.T) {
	// Component outputs should be populated with output names from HCL declarations.
	components := []PulumiResource{
		{PulumiResourceID: PulumiResourceID{Name: "named-pet", Type: "terraform:module/namedPet:NamedPet"}},
	}
	tree := []*componentNode{
		{name: "named_pet", resourceName: "named-pet", typeToken: "terraform:module/namedPet:NamedPet"},
	}

	_, err := populateComponentsFromHCL(components, tree, nil, nil, "hcl/testdata/root_with_pet", true, nil)
	require.NoError(t, err)

	outputs := components[0].Outputs
	require.NotNil(t, outputs)
	require.Contains(t, outputs, resource.PropertyKey("name"))
	require.Contains(t, outputs, resource.PropertyKey("separator"))
	// Values are empty strings (placeholders — module outputs aren't in TF state)
	require.Equal(t, resource.NewStringProperty(""), outputs["name"])
}

func TestPopulateComponentsFromHCL_OutputValuesEvaluated(t *testing.T) {
	// Output expressions should be evaluated using module-scoped resource attrs.
	// pet_module has: output "name" { value = random_pet.this.id }
	// and output "separator" { value = random_pet.this.separator }
	components := []PulumiResource{
		{PulumiResourceID: PulumiResourceID{Name: "consumer", Type: "terraform:module/consumer:Consumer"}},
	}
	tree := []*componentNode{
		{name: "consumer", resourceName: "consumer", typeToken: "terraform:module/consumer:Consumer",
			modulePath: "module.consumer"},
	}

	// Resource attrs for random_pet.this (child of the module)
	resourceAttrs := map[string]map[string]cty.Value{
		"random_pet": {
			"this": cty.ObjectVal(map[string]cty.Value{
				"id":        cty.StringVal("base-happy-fox"),
				"prefix":    cty.StringVal("base"),
				"separator": cty.StringVal("_"),
				"length":    cty.NumberIntVal(3),
			}),
		},
	}

	_, err := populateComponentsFromHCL(components, tree, nil, nil, "hcl/testdata/root_with_resource_ref", true, resourceAttrs)
	require.NoError(t, err)

	outputs := components[0].Outputs
	require.NotNil(t, outputs)

	// output "name" { value = random_pet.this.id } → "base-happy-fox"
	require.Contains(t, outputs, resource.PropertyKey("name"))
	require.Equal(t, resource.NewStringProperty("base-happy-fox"), outputs["name"])

	// output "separator" { value = random_pet.this.separator } → "_"
	require.Contains(t, outputs, resource.PropertyKey("separator"))
	require.Equal(t, resource.NewStringProperty("_"), outputs["separator"])
}

func TestPopulateComponentsFromHCL_OutputFallbackWhenEvalFails(t *testing.T) {
	// When output expression evaluation fails (no resource attrs), fall back to empty string.
	components := []PulumiResource{
		{PulumiResourceID: PulumiResourceID{Name: "named-pet", Type: "terraform:module/namedPet:NamedPet"}},
	}
	tree := []*componentNode{
		{name: "named_pet", resourceName: "named-pet", typeToken: "terraform:module/namedPet:NamedPet",
			modulePath: "module.named_pet"},
	}

	// No resource attrs — output expressions will fail
	_, err := populateComponentsFromHCL(components, tree, nil, nil, "hcl/testdata/root_with_pet", true, nil)
	require.NoError(t, err)

	outputs := components[0].Outputs
	require.NotNil(t, outputs)
	// Output names are present but values fall back to empty strings
	require.Contains(t, outputs, resource.PropertyKey("name"))
	require.Equal(t, resource.NewStringProperty(""), outputs["name"])
}

func TestInterfaceToCty(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected cty.Value
	}{
		{"string", "hello", cty.StringVal("hello")},
		{"bool", true, cty.BoolVal(true)},
		{"float64", float64(42), cty.NumberFloatVal(42)},
		{"nil", nil, cty.NullVal(cty.DynamicPseudoType)},
		{"empty_slice", []interface{}{}, cty.EmptyTupleVal},
		{"string_slice", []interface{}{"a", "b"}, cty.TupleVal([]cty.Value{cty.StringVal("a"), cty.StringVal("b")})},
		{"empty_map", map[string]interface{}{}, cty.EmptyObjectVal},
		{"string_map", map[string]interface{}{"k": "v"}, cty.ObjectVal(map[string]cty.Value{"k": cty.StringVal("v")})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := interfaceToCty(tt.input)
			require.True(t, result.RawEquals(tt.expected), "got %s, want %s", result.GoString(), tt.expected.GoString())
		})
	}
}
