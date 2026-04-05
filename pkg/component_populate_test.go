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
	"testing"

	hclpkg "github.com/pulumi/pulumi-tool-terraform-migrate/pkg/hcl"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tofu"
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

	metadata, err := populateComponentsFromHCL(components, tree, nil, nil, "hcl/testdata/root_with_pet", true, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, metadata) // metadata always returned when HCL sources available

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

func TestPopulateComponentsFromHCL_VariableDefaultsNotMerged(t *testing.T) {
	// Use "pet" module call which only passes prefix — defaults for separator and length
	// should NOT be merged into state (they belong in component-schemas.json only).
	// pet has count=2 so we test with pet-0 instance.
	components := []PulumiResource{
		{PulumiResourceID: PulumiResourceID{Name: "pet-0", Type: "terraform:module/pet:Pet"}},
	}
	tree := []*componentNode{
		{name: "pet", key: "0", resourceName: "pet-0", typeToken: "terraform:module/pet:Pet"},
	}

	metadata, err := populateComponentsFromHCL(components, tree, nil, nil, "hcl/testdata/root_with_pet", true, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, metadata)

	inputs := components[0].Inputs
	require.NotNil(t, inputs)

	// prefix was passed as "test-${count.index}" → evaluated with count.index=0 → "test-0"
	require.Contains(t, inputs, resource.PropertyKey("prefix"))
	require.Equal(t, resource.NewStringProperty("test-0"), inputs["prefix"])

	// separator and length were NOT in call site — they should NOT appear in state
	require.NotContains(t, inputs, resource.PropertyKey("separator"))
	require.NotContains(t, inputs, resource.PropertyKey("length"))

	// Only 1 input (prefix) should be present
	require.Len(t, inputs, 1)
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

	metadata, err := populateComponentsFromHCL(components, tree, nil, nil, "hcl/testdata/root_with_pet", false, nil, nil)
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

	// Simulate TF state with a random_pet.base resource (root-scoped)
	scopedAttrs := scopedResourceAttrs{
		"": {
			"random_pet": {
				"base": cty.ObjectVal(map[string]cty.Value{
					"id":        cty.StringVal("base-happy-fox"),
					"prefix":    cty.StringVal("base"),
					"separator": cty.StringVal("-"),
				}),
			},
		},
	}

	metadata, err := populateComponentsFromHCL(components, tree, nil, nil, "hcl/testdata/root_with_resource_ref", true, scopedAttrs, nil)
	require.NoError(t, err)
	require.NotNil(t, metadata)

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

	metadata, err := populateComponentsFromHCL(components, tree, nil, nil, "hcl/testdata/root_with_resource_ref", true, nil, nil)
	require.NoError(t, err) // Should not error — just warn and skip unresolvable inputs
	require.NotNil(t, metadata)

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

	_, err := populateComponentsFromHCL(components, tree, nil, nil, "hcl/testdata/root_with_pet", true, nil, nil)
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
	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_resource_ref.json",
	})
	require.NoError(t, err)

	components := []PulumiResource{
		{PulumiResourceID: PulumiResourceID{Name: "consumer", Type: "terraform:module/consumer:Consumer"}},
	}
	tree := []*componentNode{
		{name: "consumer", resourceName: "consumer", typeToken: "terraform:module/consumer:Consumer",
			modulePath: "module.consumer"},
	}

	scopedAttrs := buildScopedResourceAttrMap(tfState)
	_, err = populateComponentsFromHCL(components, tree, nil, nil, "hcl/testdata/root_with_resource_ref", true, scopedAttrs, tfState)
	require.NoError(t, err)

	outputs := components[0].Outputs
	require.NotNil(t, outputs)

	// output "name" { value = random_pet.this.id } → resolved from module.consumer.random_pet.this
	require.Contains(t, outputs, resource.PropertyKey("name"))
	// The actual value depends on the deployed state — just verify it's not empty
	require.NotEqual(t, resource.NewStringProperty(""), outputs["name"])

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
	_, err := populateComponentsFromHCL(components, tree, nil, nil, "hcl/testdata/root_with_pet", true, nil, nil)
	require.NoError(t, err)

	outputs := components[0].Outputs
	require.NotNil(t, outputs)
	// Output names are present but values fall back to empty strings
	require.Contains(t, outputs, resource.PropertyKey("name"))
	require.Equal(t, resource.NewStringProperty(""), outputs["name"])
}

func TestParseResourceAddress(t *testing.T) {
	tests := []struct {
		address    string
		modulePath string
		resType    string
		resName    string
	}{
		{"aws_s3_bucket.mybucket", "", "aws_s3_bucket", "mybucket"},
		{"module.vpc.aws_vpc.this", "module.vpc", "aws_vpc", "this"},
		{"module.vpc.module.subnets.aws_subnet.this", "module.vpc.module.subnets", "aws_subnet", "this"},
	}
	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			mod, typ, name := parseResourceAddress(tt.address)
			require.Equal(t, tt.modulePath, mod)
			require.Equal(t, tt.resType, typ)
			require.Equal(t, tt.resName, name)
		})
	}
}

func TestScopedResourceAttrs_ForModule(t *testing.T) {
	// Build scoped attrs from the indexed modules state (has module.pet[0] and module.pet[1])
	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_indexed_modules.json",
	})
	require.NoError(t, err)

	scoped := buildScopedResourceAttrMap(tfState)

	// module.pet[0] should have random_pet.this with prefix containing "test-0"
	pet0 := scoped.forModule("module.pet[0]")
	require.NotNil(t, pet0, "should find attrs for module.pet[0]")
	require.Contains(t, pet0, "random_pet")
	require.Contains(t, pet0["random_pet"], "this")

	pet0Prefix := pet0["random_pet"]["this"].GetAttr("prefix")
	require.True(t, pet0Prefix.IsKnown())
	require.Equal(t, "test-0", pet0Prefix.AsString())

	// module.pet[1] should have different prefix
	pet1 := scoped.forModule("module.pet[1]")
	require.NotNil(t, pet1)
	pet1Prefix := pet1["random_pet"]["this"].GetAttr("prefix")
	require.Equal(t, "test-1", pet1Prefix.AsString())

	// Root module should be empty (no root resources in this fixture)
	root := scoped.forModule("")
	require.Nil(t, root)
}

func TestBuildNullAttributeTemplate(t *testing.T) {
	// When existing instances have attributes, template should have same attrs with "" values
	instances := map[string]cty.Value{
		"this[0]": cty.ObjectVal(map[string]cty.Value{
			"id":     cty.StringVal("subnet-123"),
			"arn":    cty.StringVal("arn:aws:..."),
			"vpc_id": cty.StringVal("vpc-456"),
		}),
	}
	template := buildNullAttributeTemplate(instances, "", "", "")
	require.True(t, template.Type().IsObjectType())
	require.Equal(t, cty.StringVal(""), template.GetAttr("id"))
	require.Equal(t, cty.StringVal(""), template.GetAttr("arn"))
	require.Equal(t, cty.StringVal(""), template.GetAttr("vpc_id"))
}

func TestBuildNullAttributeTemplate_NoInstances(t *testing.T) {
	// When no instances exist, buildNullAttributeTemplate should accept a sourcePath
	// and resource type/name to discover attrs from HCL, rather than returning EmptyObjectVal.
	template := buildNullAttributeTemplate(map[string]cty.Value{}, "hcl/testdata/pet_module", "random_pet", "this")
	// Should NOT be EmptyObjectVal — that panics on attribute access
	require.False(t, template.RawEquals(cty.EmptyObjectVal),
		"should not use EmptyObjectVal — attribute access panics on it")
	// Should have attrs discovered from the HCL resource block
	require.True(t, template.Type().IsObjectType())
	require.True(t, template.Type().HasAttribute("prefix"))
	require.True(t, template.Type().HasAttribute("separator"))
	require.True(t, template.Type().HasAttribute("length"))
}

func TestBuildNullAttributeTemplate_NoInstances_NoSource(t *testing.T) {
	// When no source path is provided, falls back to EmptyObjectVal
	template := buildNullAttributeTemplate(map[string]cty.Value{}, "", "", "")
	require.True(t, template.RawEquals(cty.EmptyObjectVal))
}

func TestBuildChildModuleOutputs(t *testing.T) {
	parent := &componentNode{
		name:         "rdsdb",
		resourceName: "rdsdb",
		children: []*componentNode{
			{name: "db_instance", resourceName: "db_instance"},
			{name: "db_subnet_group", resourceName: "db_subnet_group"},
		},
	}
	moduleOutputValues := map[string]map[string]cty.Value{
		"db_instance":     {"address": cty.StringVal("mydb.rds.amazonaws.com")},
		"db_subnet_group": {"id": cty.StringVal("sg-123")},
	}

	childOutputs := buildChildModuleOutputs(parent, moduleOutputValues, nil, nil)
	require.NotNil(t, childOutputs)
	require.Len(t, childOutputs, 2)
	require.Equal(t, cty.StringVal("mydb.rds.amazonaws.com"), childOutputs["db_instance"]["address"])
	require.Equal(t, cty.StringVal("sg-123"), childOutputs["db_subnet_group"]["id"])
}

func TestBuildChildModuleOutputs_EmptyChildWithSource(t *testing.T) {
	// When a child module has no outputs in moduleOutputValues (e.g., zero-instance
	// or no managed resources in state) but HAS a resolved source with output
	// declarations, it should still appear with output names as empty strings.
	parent := &componentNode{
		name:         "rdsdb",
		resourceName: "rdsdb",
		children: []*componentNode{
			{name: "db_instance", resourceName: "db_instance"},
		},
	}
	resolvedSources := map[string]string{
		"module.db_instance": "testdata/../hcl/testdata/pet_module", // has outputs: name, separator
	}

	result := buildChildModuleOutputs(parent, map[string]map[string]cty.Value{}, resolvedSources, nil)
	require.NotNil(t, result, "should have outputs even for empty children with known source")
	require.Contains(t, result, "db_instance")
	require.Contains(t, result["db_instance"], "name")
	require.Contains(t, result["db_instance"], "separator")
	// Values should be empty strings (placeholder)
	require.Equal(t, cty.StringVal(""), result["db_instance"]["name"])
}

func TestBuildChildModuleOutputs_CacheFallbackForMissingChild(t *testing.T) {
	// When a child module doesn't appear in the component tree (e.g., data-source-only
	// module with no managed resources), but IS in the module cache, its outputs
	// should still be discovered.
	parent := &componentNode{
		name:         "rdsdb",
		resourceName: "rdsdb",
		modulePath:   "module.rdsdb",
		// No children — db_instance has no managed resources in state
	}
	cachedModuleSources := map[string]string{
		"module.rdsdb.module.db_instance": "testdata/../hcl/testdata/pet_module", // has outputs: name, separator
	}

	result := buildChildModuleOutputs(parent, map[string]map[string]cty.Value{}, nil, cachedModuleSources)
	require.NotNil(t, result, "should discover child outputs from module cache")
	require.Contains(t, result, "db_instance")
	require.Contains(t, result["db_instance"], "name")
	require.Contains(t, result["db_instance"], "separator")
}

func TestBuildChildModuleOutputs_NoChildren(t *testing.T) {
	node := &componentNode{name: "vpc", resourceName: "vpc"}
	result := buildChildModuleOutputs(node, nil, nil, nil)
	require.Nil(t, result)
}

func TestBuildMetaArgContext_AlwaysSetsEach(t *testing.T) {
	// Numeric keys should set BOTH count and each
	vars := buildMetaArgContext("0")
	require.Contains(t, vars, "count")
	require.Contains(t, vars, "each")
	require.Equal(t, cty.StringVal("0"), vars["each"].GetAttr("key"))

	// String keys should set each (and no count)
	vars2 := buildMetaArgContext("us-east-1")
	_, hasCount := vars2["count"]
	require.False(t, hasCount)
	require.Contains(t, vars2, "each")
	require.Equal(t, cty.StringVal("us-east-1"), vars2["each"].GetAttr("key"))
}

func TestPopulateComponentsFromHCL_NestedCallSiteUsesParentVars(t *testing.T) {
	// Test that nested module call sites are evaluated with the parent module's
	// var scope, not the root tfvars. The fixture has:
	//   root: var.database_identifier = "mydb", passes it to parent as db_name
	//   parent: var.db_name (from root), passes var.db_name to child as "name"
	//   child: var.name (from parent)
	//
	// Key: the root has NO var called "db_name" — only "database_identifier".
	// If the child call site is evaluated with root tfvars, var.db_name is missing.
	// It must be evaluated with the parent's input scope where db_name="mydb".
	components := []PulumiResource{
		{PulumiResourceID: PulumiResourceID{Name: "parent", Type: "terraform:module/parent:Parent"}},
		{PulumiResourceID: PulumiResourceID{Name: "child", Type: "terraform:module/child:Child"}},
	}
	tree := []*componentNode{
		{
			name: "parent", resourceName: "parent", typeToken: "terraform:module/parent:Parent",
			modulePath: "module.parent",
			children: []*componentNode{
				{name: "child", resourceName: "child", typeToken: "terraform:module/child:Child",
					modulePath: "module.parent.module.child"},
			},
		},
	}

	metadata, err := populateComponentsFromHCL(
		components, tree, nil, nil,
		"hcl/testdata/parent_with_nested_module", true, nil, nil,
	)
	require.NoError(t, err)
	require.NotNil(t, metadata)

	// Parent should get db_name="mydb" from root var.database_identifier
	parentInputs := components[0].Inputs
	require.NotNil(t, parentInputs, "parent should have inputs")
	require.Equal(t, resource.NewStringProperty("mydb"), parentInputs["db_name"])

	// Child should get name="mydb" — evaluated from parent's var.db_name, NOT root tfvars
	childInputs := components[1].Inputs
	require.NotNil(t, childInputs, "child should have inputs from parent var scope")
	require.Contains(t, childInputs, resource.PropertyKey("name"))
	require.Equal(t, resource.NewStringProperty("mydb"), childInputs["name"])
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

func TestPopulateComponentsFromHCL_PrePassChildModuleOutputs(t *testing.T) {
	// Parent module has: output "child_val" { value = module.child.val }
	// Child module has: output "val" { value = "hello-from-child" }
	// Consumer module has: input name = module.parent.child_val
	//
	// Without the pre-pass building child module outputs for parent modules,
	// module.parent.child_val won't resolve, so consumer's input "name" will be missing.
	components := []PulumiResource{
		{PulumiResourceID: PulumiResourceID{Name: "parent", Type: "terraform:module/parent:Parent"}},
		{PulumiResourceID: PulumiResourceID{Name: "child", Type: "terraform:module/child:Child"}},
		{PulumiResourceID: PulumiResourceID{Name: "consumer", Type: "terraform:module/consumer:Consumer"}},
	}
	tree := []*componentNode{
		{
			name: "parent", resourceName: "parent", typeToken: "terraform:module/parent:Parent",
			modulePath: "module.parent",
			children: []*componentNode{
				{name: "child", resourceName: "child", typeToken: "terraform:module/child:Child",
					modulePath: "module.parent.module.child"},
			},
		},
		{name: "consumer", resourceName: "consumer", typeToken: "terraform:module/consumer:Consumer",
			modulePath: "module.consumer"},
	}

	_, err := populateComponentsFromHCL(
		components, tree, nil, nil,
		"hcl/testdata/root_with_parent_child_output", true, nil, nil,
	)
	require.NoError(t, err)

	// Consumer's input "name" should be "hello-from-child" — resolved through
	// parent's output which references child's output via module.child.val
	consumerInputs := components[2].Inputs
	require.NotNil(t, consumerInputs, "consumer should have inputs resolved from module.parent.child_val")
	require.Contains(t, consumerInputs, resource.PropertyKey("name"))
	require.Equal(t, resource.NewStringProperty("hello-from-child"), consumerInputs["name"])
}

func TestPopulateComponentsFromHCL_NullDefaultsConvertedToZeroValues(t *testing.T) {
	// When a parent module variable has a null default (default = null with type = string),
	// and a nested child call site passes that var, coalesce(var.x, var.y) should not
	// fail because of null type mismatch. The null should be converted to "" for strings.
	//
	// Fixture: parent has var.optional_name (default=null, type=string) and var.fallback_name (default="fallback")
	//          child call: name = coalesce(var.optional_name, var.fallback_name)
	components := []PulumiResource{
		{PulumiResourceID: PulumiResourceID{Name: "parent", Type: "terraform:module/parent:Parent"}},
		{PulumiResourceID: PulumiResourceID{Name: "child", Type: "terraform:module/child:Child"}},
	}
	tree := []*componentNode{
		{
			name: "parent", resourceName: "parent", typeToken: "terraform:module/parent:Parent",
			modulePath: "module.parent",
			children: []*componentNode{
				{name: "child", resourceName: "child", typeToken: "terraform:module/child:Child",
					modulePath: "module.parent.module.child"},
			},
		},
	}

	_, err := populateComponentsFromHCL(
		components, tree, nil, nil,
		"hcl/testdata/root_with_coalesce_null", true, nil, nil,
	)
	require.NoError(t, err)

	// Child should get name="fallback" — coalesce("", "fallback") = "fallback"
	// (null default was converted to "" so coalesce doesn't reject mixed types)
	childInputs := components[1].Inputs
	require.NotNil(t, childInputs, "child should have inputs resolved through coalesce")
	require.Contains(t, childInputs, resource.PropertyKey("name"))
	require.Equal(t, resource.NewStringProperty("fallback"), childInputs["name"])
}

func TestRegisterMissingResourceTypes_AllMissingAsEmptyTuple(t *testing.T) {
	// When ALL instances of a resource type+name are missing from state,
	// registerMissingResourceTypes should register them as cty.EmptyTupleVal
	// so splat expressions and for-each iterate over an empty collection.

	// sourcePath with resource block "aws_route_table_association" "redshift" (count=0)
	// and output using splat: aws_route_table_association.redshift[*].id
	sourcePath := "hcl/testdata/resource_with_count"

	moduleResourceAttrs := map[string]map[string]cty.Value{} // nothing in state
	evalCtx := hclpkg.NewEvalContext(nil, moduleResourceAttrs, nil)

	registerMissingResourceTypes(sourcePath, moduleResourceAttrs, evalCtx, "vpc")

	// Verify by evaluating the output that uses a splat on the missing resource.
	// If registered as EmptyTupleVal, the splat resolves to [].
	outputs, err := hclpkg.ParseModuleOutputs(sourcePath)
	require.NoError(t, err)
	require.Len(t, outputs, 1)

	val, evalErr := evalCtx.EvaluateExpression(outputs[0].Expression)
	require.NoError(t, evalErr, "splat on zero-instance resource should not error")
	require.True(t, val.Type().IsTupleType(), "splat result should be a tuple")
	require.Equal(t, 0, val.LengthInt(), "splat on zero-instance resource should be empty")
}

func TestCtyNullToZero(t *testing.T) {
	tests := []struct {
		name     string
		input    cty.Value
		expected cty.Value
	}{
		{"null_string", cty.NullVal(cty.String), cty.StringVal("")},
		{"null_number", cty.NullVal(cty.Number), cty.NumberIntVal(0)},
		{"null_bool", cty.NullVal(cty.Bool), cty.BoolVal(false)},
		{"null_dynamic", cty.NullVal(cty.DynamicPseudoType), cty.StringVal("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ctyNullToZero(tt.input)
			require.True(t, result.RawEquals(tt.expected), "got %s, want %s", result.GoString(), tt.expected.GoString())
		})
	}
}
