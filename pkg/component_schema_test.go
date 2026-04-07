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

	"github.com/stretchr/testify/require"
)

func TestLoadComponentSchema(t *testing.T) {
	t.Parallel()
	iface, err := LoadComponentSchema("testdata/schemas/vpc_component_schema.json", "myproject:index:VpcComponent")
	require.NoError(t, err)
	require.Len(t, iface.Inputs, 2)
	require.Len(t, iface.Outputs, 1)

	cidr := findField(iface.Inputs, "cidr")
	require.NotNil(t, cidr)
	require.True(t, cidr.Required)

	name := findField(iface.Inputs, "name")
	require.NotNil(t, name)
	require.True(t, name.Required)

	vpcId := findField(iface.Outputs, "vpcId")
	require.NotNil(t, vpcId)
}

func TestLoadComponentSchema_NotFound(t *testing.T) {
	t.Parallel()
	_, err := LoadComponentSchema("testdata/schemas/vpc_component_schema.json", "myproject:index:DoesNotExist")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestLoadComponentSchema_NotComponent(t *testing.T) {
	t.Parallel()
	_, err := LoadComponentSchema("testdata/schemas/vpc_component_schema.json", "myproject:index:NotAComponent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a component")
}

func TestLoadComponentSchema_FileNotFound(t *testing.T) {
	t.Parallel()
	_, err := LoadComponentSchema("testdata/schemas/nonexistent.json", "myproject:index:VpcComponent")
	require.Error(t, err)
}

func TestValidateAgainstSchema_Match(t *testing.T) {
	t.Parallel()
	schema := &ComponentInterface{
		Inputs: []ComponentField{
			{Name: "cidr", Required: true},
			{Name: "name", Required: true},
		},
		Outputs: []ComponentField{
			{Name: "vpcId"},
		},
	}
	parsed := &ComponentInterface{
		Inputs: []ComponentField{
			{Name: "cidr"},
			{Name: "name"},
		},
		Outputs: []ComponentField{
			{Name: "vpcId"},
		},
	}
	err := ValidateAgainstSchema(parsed, schema)
	require.NoError(t, err)
}

func TestValidateAgainstSchema_MissingRequiredInput(t *testing.T) {
	t.Parallel()
	schema := &ComponentInterface{
		Inputs: []ComponentField{
			{Name: "cidr", Required: true},
			{Name: "name", Required: true},
		},
	}
	parsed := &ComponentInterface{
		Inputs: []ComponentField{
			{Name: "cidr"},
		},
	}
	err := ValidateAgainstSchema(parsed, schema)
	require.Error(t, err)
	require.Contains(t, err.Error(), "name")
	require.Contains(t, err.Error(), "required")
}

func TestValidateAgainstSchema_ExtraOutput(t *testing.T) {
	t.Parallel()
	schema := &ComponentInterface{
		Outputs: []ComponentField{
			{Name: "vpcId"},
		},
	}
	parsed := &ComponentInterface{
		Outputs: []ComponentField{
			{Name: "vpcId"},
			{Name: "extraField"},
		},
	}
	err := ValidateAgainstSchema(parsed, schema)
	require.Error(t, err)
	require.Contains(t, err.Error(), "extraField")
	require.Contains(t, err.Error(), "not in schema")
}

func TestValidateAgainstSchema_OptionalInputMissing(t *testing.T) {
	t.Parallel()
	schema := &ComponentInterface{
		Inputs: []ComponentField{
			{Name: "cidr", Required: true},
			{Name: "tags", Required: false},
		},
	}
	parsed := &ComponentInterface{
		Inputs: []ComponentField{
			{Name: "cidr"},
		},
	}
	err := ValidateAgainstSchema(parsed, schema)
	require.NoError(t, err)
}

func findField(fields []ComponentField, name string) *ComponentField {
	for i := range fields {
		if fields[i].Name == name {
			return &fields[i]
		}
	}
	return nil
}
