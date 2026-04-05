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
	"os"
	"strings"
	"testing"

	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tofu"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/stretchr/testify/require"
)

// --- DNS-to-DB Stack (Fixture 1) ---
// Real-world AWS stack with ~90 managed resources across 18 module instances,
// including for_each instances, nested submodules, and a data-source-only module.

func loadDnsToDbState(t *testing.T) *TranslateStateResult {
	t.Helper()
	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_dns_to_db.json",
	})
	require.NoError(t, err)
	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", true, true, nil, nil, nil, "")
	require.NoError(t, err)
	return data
}

func classifyResources(t *testing.T, data *TranslateStateResult) (stack []apitype.ResourceV3, providers []apitype.ResourceV3, components []apitype.ResourceV3, custom []apitype.ResourceV3) {
	t.Helper()
	for _, r := range data.Export.Deployment.Resources {
		switch {
		case string(r.Type) == "pulumi:pulumi:Stack":
			stack = append(stack, r)
		case r.Custom && r.Provider != "":
			// provider resources have no provider ref, custom resources do
			custom = append(custom, r)
		case r.Custom && r.Provider == "":
			providers = append(providers, r)
		default:
			components = append(components, r)
		}
	}
	return
}

func TestConvertDnsToDb_WithHCLAndModuleCache(t *testing.T) {
	t.Parallel()

	// Skip if module cache doesn't exist (requires tofu init)
	if _, err := os.Stat("testdata/tf_dns_to_db/.terraform/modules/modules.json"); os.IsNotExist(err) {
		t.Skip("skipping: requires tofu init on tf_dns_to_db fixture (run: cd pkg/testdata/tf_dns_to_db && tofu init -backend=false)")
	}

	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_dns_to_db.json",
	})
	require.NoError(t, err)

	// Pass tfSourceDir to enable HCL parsing + module cache resolution
	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", true, true, nil, nil, nil, "testdata/tf_dns_to_db")
	require.NoError(t, err)

	_, _, components, _ := classifyResources(t, data)
	require.Equal(t, 18, len(components), "expected 18 component resources")

	// With module cache, most components should have populated inputs
	withInputs := 0
	withOutputs := 0
	for _, c := range components {
		if len(c.Inputs) > 0 {
			withInputs++
		}
		if len(c.Outputs) > 0 {
			withOutputs++
		}
	}
	// Top-level modules should have inputs (from call-site eval with locals, data, tfvars, module cross-refs)
	require.GreaterOrEqual(t, withInputs, 10, "at least 10 components should have populated inputs with module cache")

	// Check component-schemas.json metadata is returned
	require.NotNil(t, data.ComponentMetadata, "should return component schema metadata")
	require.GreaterOrEqual(t, len(data.ComponentMetadata.Components), 10, "metadata should have entries for most modules")

	// Verify metadata has Pulumi-formatted types for inputs that declare them
	typedInputs := 0
	for _, schema := range data.ComponentMetadata.Components {
		for _, inp := range schema.Inputs {
			if inp.Type != nil {
				typedInputs++
			}
		}
	}
	require.Greater(t, typedInputs, 0, "at least some inputs should have Pulumi-formatted types")
}

func TestConvertDnsToDb_EvalWarningCount(t *testing.T) {
	// NOT parallel — temporarily redirects os.Stderr to count warnings.
	if _, err := os.Stat("testdata/tf_dns_to_db/.terraform/modules/modules.json"); os.IsNotExist(err) {
		t.Skip("skipping: requires tofu init on tf_dns_to_db fixture (run: cd pkg/testdata/tf_dns_to_db && tofu init -backend=false)")
	}

	// Capture stderr to count Warning lines
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w

	ctx := context.Background()
	tfState, loadErr := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_dns_to_db.json",
	})
	require.NoError(t, loadErr)

	_, translateErr := TranslateState(ctx, tfState, nil, "dev", "test-project", true, true, nil, nil, nil, "testdata/tf_dns_to_db")
	require.NoError(t, translateErr)

	// Restore stderr and read captured output
	w.Close()
	os.Stderr = origStderr
	buf := make([]byte, 1<<20) // 1MB
	n, _ := r.Read(buf)
	captured := buf[:n]
	r.Close()

	// Count and log Warning lines so we can act on them
	var warnings []string
	for _, line := range strings.Split(string(captured), "\n") {
		if strings.Contains(line, "Warning:") {
			warnings = append(warnings, line)
		}
	}

	t.Logf("DNS-to-DB eval warnings: %d", len(warnings))
	for i, w := range warnings {
		t.Logf("  [%d] %s", i+1, w)
	}
	// Was 68 → 12 → 2 (only templatefile missing file warnings remain).
	require.Less(t, len(warnings), 5, "eval warning count regressed (was 68, now 2; only templatefile warnings remain)")
}

func TestConvertDnsToDb(t *testing.T) {
	t.Parallel()
	data := loadDnsToDbState(t)

	var components []apitype.ResourceV3
	var customResources []apitype.ResourceV3
	var rootResources []apitype.ResourceV3

	stackURN := ""
	for _, r := range data.Export.Deployment.Resources {
		if string(r.Type) == "pulumi:pulumi:Stack" {
			stackURN = string(r.URN)
			continue
		}
		if !r.Custom {
			components = append(components, r)
			continue
		}
		// Check if this is a provider resource (type starts with "pulumi:providers:")
		if isProvider(r) {
			continue
		}
		customResources = append(customResources, r)
		if string(r.Parent) == stackURN {
			rootResources = append(rootResources, r)
		}
	}

	// 18 component instances (19 modules minus db_instance which has only data sources)
	require.Len(t, components, 18, "expected 18 component resources")

	// ~90 managed resources
	require.GreaterOrEqual(t, len(customResources), 85, "expected ~90 managed resources")

	// Root resources (not in any module) should be parented to Stack
	require.GreaterOrEqual(t, len(rootResources), 5, "expected root resources parented to Stack")

	// Verify for_each instances share the same type token
	componentTypes := map[string][]string{} // type → names
	for _, c := range components {
		componentTypes[string(c.Type)] = append(componentTypes[string(c.Type)], string(c.URN))
	}
	// ec2_private_app1 has 2 instances → same type token
	app1Type := "terraform:module/ec2PrivateApp1:Ec2PrivateApp1"
	require.Len(t, componentTypes[app1Type], 2, "ec2_private_app1 should have 2 for_each instances")

	// Verify nested module (rdsdb submodules) produce $-delimited URN type chain
	var rdsdbChildren []apitype.ResourceV3
	for _, c := range components {
		urn := string(c.URN)
		if contains(urn, "module/rdsdb:Rdsdb$") {
			rdsdbChildren = append(rdsdbChildren, c)
		}
	}
	// db_option_group, db_parameter_group, db_subnet_group (not db_instance — data sources only)
	require.Len(t, rdsdbChildren, 3, "rdsdb should have 3 nested component children (db_instance skipped — data only)")
}

func TestConvertDnsToDb_TypeOverrides(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_dns_to_db.json",
	})
	require.NoError(t, err)

	typeOverrides := map[string]string{
		"module.vpc":              "myinfra:network:Vpc",
		"module.ec2_private_app1": "myinfra:compute:AppServer",
	}

	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", true, true, typeOverrides, nil, nil, "")
	require.NoError(t, err)

	var components []apitype.ResourceV3
	for _, r := range data.Export.Deployment.Resources {
		if !r.Custom && string(r.Type) != "pulumi:pulumi:Stack" {
			components = append(components, r)
		}
	}

	// vpc should have custom type
	vpcFound := false
	for _, c := range components {
		if string(c.Type) == "myinfra:network:Vpc" {
			vpcFound = true
		}
	}
	require.True(t, vpcFound, "vpc should have overridden type myinfra:network:Vpc")

	// Both ec2_private_app1 instances should have the custom type
	app1Count := 0
	for _, c := range components {
		if string(c.Type) == "myinfra:compute:AppServer" {
			app1Count++
		}
	}
	require.Equal(t, 2, app1Count, "both ec2_private_app1 for_each instances should have custom type")

	// Other modules should keep derived types
	sgFound := false
	for _, c := range components {
		if string(c.Type) == "terraform:module/publicBastionSg:PublicBastionSg" {
			sgFound = true
		}
	}
	require.True(t, sgFound, "public_bastion_sg should keep auto-derived type")
}

func TestConvertDnsToDb_FlatMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_dns_to_db.json",
	})
	require.NoError(t, err)

	// enableComponents=false → flat mode
	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", false, true, nil, nil, nil, "")
	require.NoError(t, err)

	// No component resources in flat mode
	for _, r := range data.Export.Deployment.Resources {
		if !r.Custom && string(r.Type) != "pulumi:pulumi:Stack" {
			t.Fatalf("unexpected component resource in flat mode: %s", r.Type)
		}
	}

	// All managed resources should be parented to Stack
	stackURN := ""
	for _, r := range data.Export.Deployment.Resources {
		if string(r.Type) == "pulumi:pulumi:Stack" {
			stackURN = string(r.URN)
			break
		}
	}
	for _, r := range data.Export.Deployment.Resources {
		if !r.Custom || isProvider(r) || string(r.Type) == "pulumi:pulumi:Stack" {
			continue
		}
		require.Equal(t, stackURN, string(r.Parent), "resource %s should be parented to Stack in flat mode", r.URN)
	}
}

// --- Helper functions ---

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func isProvider(r apitype.ResourceV3) bool {
	const prefix = "pulumi:providers:"
	return len(r.Type) >= len(prefix) && string(r.Type)[:len(prefix)] == prefix
}

// --- Multi-resource module (Fixture 2) ---

func TestConvertMultiResourceModule(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_multi_resource_module.json",
	})
	require.NoError(t, err)

	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", true, false, nil, nil, nil, "")
	require.NoError(t, err)

	_, _, components, custom := classifyResources(t, data)

	// 1 component: zoo
	require.Len(t, components, 1, "expected 1 component (zoo)")
	zoo := components[0]
	require.Equal(t, "terraform:module/zoo:Zoo", string(zoo.Type))

	// 3 child resources parented to zoo
	zooURN := string(zoo.URN)
	var children []apitype.ResourceV3
	for _, r := range custom {
		if string(r.Parent) == zooURN {
			children = append(children, r)
		}
	}
	require.Len(t, children, 3, "expected 3 child resources (random_pet, random_string, random_integer)")
}

func TestConvertMultiResourceModule_WithHCL(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_multi_resource_module.json",
	})
	require.NoError(t, err)

	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", true, true, nil, nil, nil, "testdata/tf_multi_resource_module")
	require.NoError(t, err)

	_, _, components, _ := classifyResources(t, data)
	require.Len(t, components, 1)

	zoo := components[0]

	// Component inputs should contain prefix
	require.NotNil(t, zoo.Inputs, "component inputs should not be nil")
	require.Equal(t, "myprefix", zoo.Inputs["prefix"], "prefix input should be 'myprefix'")

	// Component outputs should have animal_name and tag keys
	require.NotNil(t, zoo.Outputs, "component outputs should not be nil")
	_, hasAnimalName := zoo.Outputs["animal_name"]
	require.True(t, hasAnimalName, "outputs should have animal_name key")
	_, hasTag := zoo.Outputs["tag"]
	require.True(t, hasTag, "outputs should have tag key")
}

// --- Deep nested mixed (Fixture 3) ---

func TestConvertDeepNestedMixed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_deep_nested_mixed.json",
	})
	require.NoError(t, err)

	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", true, true, nil, nil, nil, "")
	require.NoError(t, err)

	_, _, components, _ := classifyResources(t, data)

	// 10 total components: 2 env + 4 svc + 4 instance
	require.Len(t, components, 10, "expected 10 components (2 env + 4 svc + 4 instance)")

	// URN type chain should use $ for nesting (full form: ...Env$terraform:module/svc:Svc$terraform:module/instance:Instance)
	foundNestedType := false
	for _, c := range components {
		urn := string(c.URN)
		if contains(urn, "Svc$terraform:module/instance:Instance") {
			foundNestedType = true
			break
		}
	}
	require.True(t, foundNestedType, "should have $-delimited nested URN type chain")

	// env-dev should sort before env-prod (alphabetical ordering)
	devIdx := -1
	prodIdx := -1
	for i, r := range data.Export.Deployment.Resources {
		urn := string(r.URN)
		if contains(urn, "env-dev") && devIdx == -1 {
			devIdx = i
		}
		if contains(urn, "env-prod") && prodIdx == -1 {
			prodIdx = i
		}
	}
	require.Greater(t, devIdx, -1, "env-dev should be present")
	require.Greater(t, prodIdx, -1, "env-prod should be present")
	require.Less(t, devIdx, prodIdx, "env-dev should sort before env-prod")
}

func TestConvertDeepNestedMixed_FlatMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_deep_nested_mixed.json",
	})
	require.NoError(t, err)

	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", false, false, nil, nil, nil, "")
	require.NoError(t, err)

	_, _, components, _ := classifyResources(t, data)
	require.Len(t, components, 0, "flat mode should produce 0 components")

	// All managed resources should be parented to Stack
	stackURN := ""
	for _, r := range data.Export.Deployment.Resources {
		if string(r.Type) == "pulumi:pulumi:Stack" {
			stackURN = string(r.URN)
			break
		}
	}
	for _, r := range data.Export.Deployment.Resources {
		if !r.Custom || isProvider(r) || string(r.Type) == "pulumi:pulumi:Stack" {
			continue
		}
		require.Equal(t, stackURN, string(r.Parent), "resource %s should be parented to Stack in flat mode", r.URN)
	}
}

// --- Complex HCL expressions (Fixture 4) ---

func TestConvertComplexExpressions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_complex_expressions.json",
	})
	require.NoError(t, err)

	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", true, true, nil, nil, nil, "testdata/tf_complex_expressions")
	require.NoError(t, err)

	_, _, components, _ := classifyResources(t, data)

	// Find svc-0 and svc-1 components by URN
	var svc0, svc1 *apitype.ResourceV3
	for i, c := range components {
		urn := string(c.URN)
		if contains(urn, "svc-0") && !contains(urn, "svc-01") {
			svc0 = &components[i]
		}
		if contains(urn, "svc-1") {
			svc1 = &components[i]
		}
	}
	require.NotNil(t, svc0, "svc-0 component should exist")
	require.NotNil(t, svc1, "svc-1 component should exist")

	// svc-0: prefix="svc-00", is_primary=true, label="SERVICE-0"
	require.NotNil(t, svc0.Inputs)
	require.Equal(t, "svc-00", svc0.Inputs["prefix"], "svc-0 prefix should be 'svc-00'")
	require.Equal(t, true, svc0.Inputs["is_primary"], "svc-0 is_primary should be true")
	require.Equal(t, "SERVICE-0", svc0.Inputs["label"], "svc-0 label should be 'SERVICE-0'")

	// svc-1: prefix="svc-01", is_primary=false, label="SERVICE-1"
	require.NotNil(t, svc1.Inputs)
	require.Equal(t, "svc-01", svc1.Inputs["prefix"], "svc-1 prefix should be 'svc-01'")
	require.Equal(t, false, svc1.Inputs["is_primary"], "svc-1 is_primary should be false")
	require.Equal(t, "SERVICE-1", svc1.Inputs["label"], "svc-1 label should be 'SERVICE-1'")
}

// --- Tfvars resolution (Fixture 5) ---

func TestConvertTfvarsResolution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_tfvars_resolution.json",
	})
	require.NoError(t, err)

	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", true, true, nil, nil, nil, "testdata/tf_tfvars_resolution")
	require.NoError(t, err)

	_, _, components, _ := classifyResources(t, data)
	require.GreaterOrEqual(t, len(components), 1, "should have at least 1 component")

	// Component inputs should have prefix="staging" (from tfvars env="staging")
	// suffix="prod" is a variable default — should NOT be in state (belongs in component-schemas.json)
	comp := components[0]
	require.NotNil(t, comp.Inputs, "component inputs should not be nil")
	require.Equal(t, "staging", comp.Inputs["prefix"], "prefix should be 'staging' from tfvars")
	_, hasSuffix := comp.Inputs["suffix"]
	require.False(t, hasSuffix, "variable defaults should not be in state")
}

// --- Special key sanitization (Fixture 6) ---

func TestConvertSpecialKeyModules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_special_key_modules.json",
	})
	require.NoError(t, err)

	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", true, false, nil, nil, nil, "")
	require.NoError(t, err)

	_, _, components, _ := classifyResources(t, data)
	require.Len(t, components, 3, "expected 3 components")

	// Names should be sanitized
	expectedNames := map[string]bool{
		"region-us-east-1":         false,
		"region-eu-west-1-zone-a":  false,
		"region-ap-southeast-2":    false,
	}
	for _, c := range components {
		urn := string(c.URN)
		for name := range expectedNames {
			if contains(urn, name) {
				expectedNames[name] = true
			}
		}
	}
	for name, found := range expectedNames {
		require.True(t, found, "should find sanitized component name: %s", name)
	}
}

// --- Flat mode sweep (all fixtures) ---

func TestConvertFlatMode_AllFixtures(t *testing.T) {
	t.Parallel()

	fixtures := []struct {
		name      string
		stateFile string
	}{
		// dns_to_db excluded — has its own TestConvertDnsToDb_FlatMode test,
		// and parallel execution causes "text file busy" on the null provider plugin.
		{"multi_resource_module", "testdata/tofu_state_multi_resource_module.json"},
		{"deep_nested_mixed", "testdata/tofu_state_deep_nested_mixed.json"},
		{"complex_expressions", "testdata/tofu_state_complex_expressions.json"},
		{"tfvars_resolution", "testdata/tofu_state_tfvars_resolution.json"},
		{"special_key_modules", "testdata/tofu_state_special_key_modules.json"},
	}

	for _, tt := range fixtures {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
				StateFilePath: tt.stateFile,
			})
			require.NoError(t, err)

			data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", false, false, nil, nil, nil, "")
			require.NoError(t, err)

			_, _, components, _ := classifyResources(t, data)
			require.Len(t, components, 0, "flat mode should produce 0 components for %s", tt.name)
		})
	}
}
