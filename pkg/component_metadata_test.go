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

	hclpkg "github.com/pulumi/pulumi-tool-terraform-migrate/pkg/hcl"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func TestBuildComponentSchemaMetadata(t *testing.T) {
	components := []PulumiResource{
		{PulumiResourceID: PulumiResourceID{Name: "vpc", Type: "terraform:module/vpc:Vpc"}},
	}
	tree := []*componentNode{
		{name: "vpc", resourceName: "vpc", typeToken: "terraform:module/vpc:Vpc"},
	}

	defaultVal := cty.StringVal("default-vpc")
	variables := map[string][]hclpkg.ModuleVariable{
		"vpc": {
			{Name: "cidr", Type: "string", Description: "The CIDR block"},
			{Name: "name", Type: "string", Default: &defaultVal, Description: "VPC name"},
		},
	}
	outputs := map[string][]hclpkg.ModuleOutput{
		"vpc": {
			{Name: "vpc_id", Description: "The VPC ID"},
		},
	}
	sources := map[string]string{
		"module.vpc": "./modules/vpc",
	}

	metadata := buildComponentSchemaMetadata(components, tree, variables, outputs, sources)

	require.Len(t, metadata.Components, 1)
	schema, ok := metadata.Components["module.vpc"]
	require.True(t, ok)
	require.Equal(t, "terraform:module/vpc:Vpc", schema.Type)
	require.Equal(t, "./modules/vpc", schema.Source)

	// Check inputs
	require.Len(t, schema.Inputs, 2)
	cidr := schema.Inputs[0]
	require.Equal(t, "cidr", cidr.Name)
	require.Equal(t, "string", cidr.Type)
	require.True(t, cidr.Required)
	require.Nil(t, cidr.Default)

	name := schema.Inputs[1]
	require.Equal(t, "name", name.Name)
	require.False(t, name.Required)
	require.Equal(t, "default-vpc", name.Default)

	// Check outputs
	require.Len(t, schema.Outputs, 1)
	require.Equal(t, "vpc_id", schema.Outputs[0].Name)
	require.Equal(t, "The VPC ID", schema.Outputs[0].Description)
}

func TestWriteComponentSchemaMetadata(t *testing.T) {
	metadata := &ComponentSchemaMetadata{
		Components: map[string]ComponentSchema{
			"module.vpc": {
				Type:   "terraform:module/vpc:Vpc",
				Source: "./modules/vpc",
				Inputs: []ComponentFieldMeta{
					{Name: "cidr", Type: "string", Required: true},
				},
				Outputs: []ComponentFieldMeta{
					{Name: "vpc_id"},
				},
			},
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "component-schemas.json")
	err := WriteComponentSchemaMetadata(metadata, path)
	require.NoError(t, err)

	// Read back and verify
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var loaded ComponentSchemaMetadata
	err = json.Unmarshal(data, &loaded)
	require.NoError(t, err)
	require.Len(t, loaded.Components, 1)

	schema := loaded.Components["module.vpc"]
	require.Equal(t, "terraform:module/vpc:Vpc", schema.Type)
	require.Len(t, schema.Inputs, 1)
	require.Equal(t, "cidr", schema.Inputs[0].Name)
	require.True(t, schema.Inputs[0].Required)
}

func TestHclTypeToPulumiSchemaType(t *testing.T) {
	tests := []struct {
		hclType  string
		expected interface{}
	}{
		{"string", "string"},
		{"number", "number"},
		{"bool", "boolean"},
		{"any", "object"},
		{"list(string)", map[string]interface{}{"type": "array", "items": "string"}},
		{"set(string)", map[string]interface{}{"type": "array", "items": "string"}},
		{"map(string)", map[string]interface{}{"type": "object", "additionalProperties": "string"}},
		{"list(number)", map[string]interface{}{"type": "array", "items": "number"}},
		{"map(bool)", map[string]interface{}{"type": "object", "additionalProperties": "boolean"}},
		{"list(list(string))", map[string]interface{}{
			"type": "array",
			"items": map[string]interface{}{"type": "array", "items": "string"},
		}},
		{"object({name=string})", map[string]interface{}{"type": "object"}},
		{"tuple([string])", map[string]interface{}{"type": "array"}},
		{"", nil},
	}
	for _, tt := range tests {
		t.Run(tt.hclType, func(t *testing.T) {
			result := hclTypeToPulumiSchemaType(tt.hclType)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestCtyValueToInterface(t *testing.T) {
	tests := []struct {
		name     string
		input    cty.Value
		expected interface{}
	}{
		{"string", cty.StringVal("hello"), "hello"},
		{"bool", cty.True, true},
		{"int", cty.NumberIntVal(42), int64(42)},
		{"float", cty.NumberFloatVal(3.14), 3.14},
		{"null", cty.NullVal(cty.String), nil},
		{"list", cty.ListVal([]cty.Value{cty.StringVal("a"), cty.StringVal("b")}), []interface{}{"a", "b"}},
		{"map", cty.MapVal(map[string]cty.Value{"k": cty.StringVal("v")}), map[string]interface{}{"k": "v"}},
		{"empty_object", cty.EmptyObjectVal, map[string]interface{}{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ctyValueToInterface(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}
