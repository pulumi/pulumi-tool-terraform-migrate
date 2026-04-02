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

	metadata, err := populateComponentsFromHCL(components, tree, nil, nil, "hcl/testdata/root_with_pet", true)
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

	metadata, err := populateComponentsFromHCL(components, tree, nil, nil, "hcl/testdata/root_with_pet", true)
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

	metadata, err := populateComponentsFromHCL(components, tree, nil, nil, "hcl/testdata/root_with_pet", false)
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
