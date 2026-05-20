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
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pulumi/opentofu/addrs"
	"github.com/pulumi/opentofu/states"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func TestBuildModuleMap_WithoutEval(t *testing.T) {
	t.Parallel()
	tfDir, err := filepath.Abs(filepath.Join("testdata", "tf_indexed_modules"))
	require.NoError(t, err)

	config, err := LoadConfig(tfDir)
	require.NoError(t, err)

	rawState, err := LoadRawState(filepath.Join(tfDir, "terraform.tfstate"))
	require.NoError(t, err)

	// Build without eval (nil tofuCtx) — no pulumiProviders needed for URN
	// generation in this test since we just check structure.
	mm, err := BuildModuleMap(config, nil, rawState, nil, "test-stack", "test-project")
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
	assert.Equal(t, "managed", pet0.Resources[0].Mode)
	assert.Equal(t, "module.pet[0].random_pet.this", pet0.Resources[0].TranslatedURN) // falls back to address
	assert.Equal(t, "module.pet[0].random_pet.this", pet0.Resources[0].TerraformAddress)
	assert.Equal(t, "test-0-just-phoenix", pet0.Resources[0].ImportID)

	assert.Len(t, pet1.Resources, 1)
	assert.Equal(t, "managed", pet1.Resources[0].Mode)
	assert.Equal(t, "module.pet[1].random_pet.this", pet1.Resources[0].TerraformAddress)
	assert.Equal(t, "test-1-brief-jennet", pet1.Resources[0].ImportID)

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
	// NOT parallel — starts provider plugin processes via go-plugin.
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

	tofuCtx, cleanup, err := Evaluate(config, rawState, tfDir)
	require.NoError(t, err)
	defer cleanup()

	rootVars := BuildRootVariables(config, tfDir, nil)
	evalScopes, _ := BuildEvalScopes(context.Background(), tofuCtx, config, rawState, rootVars)

	mm, err := BuildModuleMap(config, evalScopes, rawState, nil, "test-stack", "test-project")
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
	t.Parallel()
	tfDir, err := filepath.Abs(filepath.Join("testdata", "tf_indexed_modules"))
	require.NoError(t, err)

	config, err := LoadConfig(tfDir)
	require.NoError(t, err)

	rawState, err := LoadRawState(filepath.Join(tfDir, "terraform.tfstate"))
	require.NoError(t, err)

	mm, err := BuildModuleMap(config, nil, rawState, nil, "test-stack", "test-project")
	require.NoError(t, err)

	pet0 := mm.Modules["pet[0]"]
	require.NotNil(t, pet0)
	require.NotNil(t, pet0.Interface)
	require.Len(t, pet0.Interface.Inputs, 1)

	// The expression for "prefix" should be the call-site expression text.
	assert.Contains(t, pet0.Interface.Inputs[0].Expression, "test-${count.index}")
}

func TestWriteModuleMap(t *testing.T) {
	t.Parallel()
	mm := &ModuleMap{
		Modules: map[string]*ModuleMapEntry{
			"vpc": {
				TerraformPath: "module.vpc",
				Source:        "./modules/vpc",
				Resources: []ModuleResource{{
					Mode:             "managed",
					TranslatedURN:    "urn:pulumi:stack::project::aws:ec2/vpc:Vpc::main",
					TerraformAddress: "module.vpc.aws_vpc.main",
					ImportID:         "vpc-12345",
				}},
				Interface: &ModuleInterface{
					Inputs:  []ModuleInterfaceField{{Name: "cidr", Required: true}},
					Outputs: []ModuleInterfaceField{{Name: "id"}},
				},
			},
		},
		RootResources: []ModuleResource{
			{
				Mode:             "managed",
				TranslatedURN:    "urn:pulumi:stack::project::aws:s3/bucket:Bucket::example",
				TerraformAddress: "aws_s3_bucket.example",
				ImportID:         "my-bucket",
			},
			{
				Mode:             "data",
				TranslatedURN:    "",
				TerraformAddress: "data.terraform_remote_state.old",
				ImportID:         "",
				Attributes:       map[string]interface{}{"backend": "s3", "workspace": "prod"},
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
	require.Len(t, got.Modules["vpc"].Resources, 1)
	assert.Equal(t, "urn:pulumi:stack::project::aws:ec2/vpc:Vpc::main", got.Modules["vpc"].Resources[0].TranslatedURN)
	assert.Equal(t, "module.vpc.aws_vpc.main", got.Modules["vpc"].Resources[0].TerraformAddress)
	assert.Equal(t, "vpc-12345", got.Modules["vpc"].Resources[0].ImportID)
	assert.Equal(t, "managed", got.Modules["vpc"].Resources[0].Mode)
	require.NotNil(t, got.Modules["vpc"].Interface)
	assert.Len(t, got.Modules["vpc"].Interface.Inputs, 1)
	assert.Equal(t, "cidr", got.Modules["vpc"].Interface.Inputs[0].Name)

	// Root resources round-trip.
	require.Len(t, got.RootResources, 2)
	assert.Equal(t, "managed", got.RootResources[0].Mode)
	assert.Equal(t, "aws_s3_bucket.example", got.RootResources[0].TerraformAddress)
	assert.Equal(t, "my-bucket", got.RootResources[0].ImportID)
	assert.Equal(t, "data", got.RootResources[1].Mode)
	assert.Equal(t, "", got.RootResources[1].TranslatedURN)
	assert.Equal(t, "data.terraform_remote_state.old", got.RootResources[1].TerraformAddress)
	require.NotNil(t, got.RootResources[1].Attributes)
	assert.Equal(t, "s3", got.RootResources[1].Attributes["backend"])
	assert.Equal(t, "prod", got.RootResources[1].Attributes["workspace"])
	assert.Nil(t, got.RootResources[0].Attributes) // this test constructs resources without attributes
}

func TestBuildModuleMap_RootResources(t *testing.T) {
	t.Parallel()
	tfDir, err := filepath.Abs(filepath.Join("testdata", "tf_indexed_modules"))
	require.NoError(t, err)

	config, err := LoadConfig(tfDir)
	require.NoError(t, err)

	rawState, err := LoadRawState(filepath.Join(tfDir, "terraform.tfstate"))
	require.NoError(t, err)

	// Add a root-level managed resource to the existing state.
	rootModule := rawState.RootModule()
	rootModule.SetResourceInstanceCurrent(
		addrs.ResourceInstance{
			Resource: addrs.Resource{
				Mode: addrs.ManagedResourceMode,
				Type: "aws_s3_bucket",
				Name: "example",
			},
			Key: addrs.NoKey,
		},
		&states.ResourceInstanceObjectSrc{
			AttrsJSON: []byte(`{"id":"my-bucket","bucket":"my-bucket"}`),
		},
		addrs.AbsProviderConfig{
			Provider: addrs.MustParseProviderSourceString("registry.opentofu.org/hashicorp/aws"),
		},
		nil,
	)

	// Add a root-level data source.
	rootModule.SetResourceInstanceCurrent(
		addrs.ResourceInstance{
			Resource: addrs.Resource{
				Mode: addrs.DataResourceMode,
				Type: "terraform_remote_state",
				Name: "old",
			},
			Key: addrs.NoKey,
		},
		&states.ResourceInstanceObjectSrc{
			AttrsJSON: []byte(`{"backend":"s3"}`),
		},
		addrs.AbsProviderConfig{
			Provider: addrs.MustParseProviderSourceString("terraform.io/builtin/terraform"),
		},
		nil,
	)

	mm, err := BuildModuleMap(config, nil, rawState, nil, "test-stack", "test-project")
	require.NoError(t, err)
	require.NotNil(t, mm)

	// Module resources should still work.
	require.Contains(t, mm.Modules, "pet[0]")

	// Root resources should be populated.
	require.NotNil(t, mm.RootResources)
	require.Len(t, mm.RootResources, 2)

	// Sort by address for deterministic assertion.
	sort.Slice(mm.RootResources, func(i, j int) bool {
		return mm.RootResources[i].TerraformAddress < mm.RootResources[j].TerraformAddress
	})

	// Managed resource — URN falls back to raw address when pulumiProviders is nil.
	assert.Equal(t, "managed", mm.RootResources[0].Mode)
	assert.Equal(t, "aws_s3_bucket.example", mm.RootResources[0].TranslatedURN)
	assert.Equal(t, "aws_s3_bucket.example", mm.RootResources[0].TerraformAddress)
	assert.Equal(t, "my-bucket", mm.RootResources[0].ImportID)

	// Data source — URN should be empty.
	assert.Equal(t, "data", mm.RootResources[1].Mode)
	assert.Equal(t, "data.terraform_remote_state.old", mm.RootResources[1].TerraformAddress)
	assert.Equal(t, "", mm.RootResources[1].TranslatedURN)
	assert.Equal(t, "", mm.RootResources[1].ImportID) // no "id" attribute
	require.NotNil(t, mm.RootResources[1].Attributes)
	assert.Equal(t, "s3", mm.RootResources[1].Attributes["backend"])
}

func TestBuildModuleMap_DataSources(t *testing.T) {
	t.Parallel()
	tfDir, err := filepath.Abs(filepath.Join("testdata", "tf_indexed_modules"))
	require.NoError(t, err)

	config, err := LoadConfig(tfDir)
	require.NoError(t, err)

	rawState, err := LoadRawState(filepath.Join(tfDir, "terraform.tfstate"))
	require.NoError(t, err)

	// Add a data source inside module.pet[0].
	petModule := rawState.Module(addrs.RootModuleInstance.Child("pet", addrs.IntKey(0)))
	require.NotNil(t, petModule)

	petModule.SetResourceInstanceCurrent(
		addrs.ResourceInstance{
			Resource: addrs.Resource{
				Mode: addrs.DataResourceMode,
				Type: "aws_caller_identity",
				Name: "current",
			},
			Key: addrs.NoKey,
		},
		&states.ResourceInstanceObjectSrc{
			AttrsJSON: []byte(`{"account_id":"123456789","id":"123456789"}`),
		},
		addrs.AbsProviderConfig{
			Provider: addrs.MustParseProviderSourceString("registry.opentofu.org/hashicorp/aws"),
		},
		nil,
	)

	mm, err := BuildModuleMap(config, nil, rawState, nil, "test-stack", "test-project")
	require.NoError(t, err)

	pet0 := mm.Modules["pet[0]"]
	require.NotNil(t, pet0)

	// Should have 2 resources: the managed random_pet and the data source.
	require.Len(t, pet0.Resources, 2)

	// Find the data source entry.
	var dataRes *ModuleResource
	for i := range pet0.Resources {
		if pet0.Resources[i].Mode == "data" {
			dataRes = &pet0.Resources[i]
			break
		}
	}
	require.NotNil(t, dataRes, "expected a data source in pet[0] resources")

	assert.Equal(t, "data", dataRes.Mode)
	assert.Equal(t, "module.pet[0].data.aws_caller_identity.current", dataRes.TerraformAddress)
	assert.Equal(t, "", dataRes.TranslatedURN)
	assert.Equal(t, "123456789", dataRes.ImportID)
	require.NotNil(t, dataRes.Attributes)
	assert.Equal(t, "123456789", dataRes.Attributes["account_id"])
	assert.Equal(t, "123456789", dataRes.Attributes["id"])

	// The managed resource should still be there.
	var managedRes *ModuleResource
	for i := range pet0.Resources {
		if pet0.Resources[i].Mode == "managed" {
			managedRes = &pet0.Resources[i]
			break
		}
	}
	require.NotNil(t, managedRes)
	assert.Equal(t, "managed", managedRes.Mode)
	assert.Equal(t, "module.pet[0].random_pet.this", managedRes.TerraformAddress)
}

func TestCtyValueToInterface(t *testing.T) {
	t.Parallel()
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

func TestRawStateFromTfjson_DataSources(t *testing.T) {
	t.Parallel()

	tfjsonState := &tfjson.State{
		FormatVersion: "1.0",
		Values: &tfjson.StateValues{
			RootModule: &tfjson.StateModule{
				Resources: []*tfjson.StateResource{
					{
						Address:      "data.terraform_remote_state.old",
						Mode:         tfjson.DataResourceMode,
						Type:         "terraform_remote_state",
						Name:         "old",
						ProviderName: "terraform.io/builtin/terraform",
						AttributeValues: map[string]interface{}{
							"backend": "s3",
						},
					},
					{
						Address:      "aws_s3_bucket.example",
						Mode:         tfjson.ManagedResourceMode,
						Type:         "aws_s3_bucket",
						Name:         "example",
						ProviderName: "registry.opentofu.org/hashicorp/aws",
						AttributeValues: map[string]interface{}{
							"id":     "my-bucket",
							"bucket": "my-bucket",
						},
					},
				},
			},
		},
	}

	state := rawStateFromTfjson(tfjsonState)

	rootModule := state.RootModule()
	require.NotNil(t, rootModule)

	dataRes := rootModule.Resource(addrs.Resource{
		Mode: addrs.DataResourceMode,
		Type: "terraform_remote_state",
		Name: "old",
	})
	require.NotNil(t, dataRes, "expected data.terraform_remote_state.old in root module")

	managedRes := rootModule.Resource(addrs.Resource{
		Mode: addrs.ManagedResourceMode,
		Type: "aws_s3_bucket",
		Name: "example",
	})
	require.NotNil(t, managedRes, "expected aws_s3_bucket.example in root module")
}
