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
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
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
	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", nil, nil, nil, "")
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
	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", nil, nil, nil, "testdata/tf_dns_to_db")
	require.NoError(t, err)

	// No component resources in flat state
	_, _, components, _ := classifyResources(t, data)
	require.Len(t, components, 0, "state is always flat — no component resources")

	// Component metadata should still be available via sidecar
	require.NotNil(t, data.ComponentMapData, "should have component map data with HCL source")
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

	_, translateErr := TranslateState(ctx, tfState, nil, "dev", "test-project", nil, nil, nil, "testdata/tf_dns_to_db")
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
	// Was 68 → 12 → 2 → 0 (fixed by resolving file/templatefile paths relative to source dir).
	require.Equal(t, 0, len(warnings), "eval warning count regressed")
}

func TestConvertDnsToDb(t *testing.T) {
	t.Parallel()
	data := loadDnsToDbState(t)

	var customResources []apitype.ResourceV3
	stackURN := ""
	for _, r := range data.Export.Deployment.Resources {
		if string(r.Type) == "pulumi:pulumi:Stack" {
			stackURN = string(r.URN)
			continue
		}
		if isProvider(r) {
			continue
		}
		if !r.Custom {
			t.Fatalf("unexpected component resource in flat state: %s", r.Type)
		}
		customResources = append(customResources, r)
	}

	// ~90 managed resources
	require.GreaterOrEqual(t, len(customResources), 85, "expected ~90 managed resources")

	// All resources should be parented to Stack (flat state)
	for _, r := range customResources {
		require.Equal(t, stackURN, string(r.Parent), "resource %s should be parented to Stack", r.URN)
	}
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

	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", typeOverrides, nil, nil, "")
	require.NoError(t, err)

	// No component resources in flat state
	for _, r := range data.Export.Deployment.Resources {
		if !r.Custom && string(r.Type) != "pulumi:pulumi:Stack" {
			t.Fatalf("unexpected component resource in flat state: %s", r.Type)
		}
	}
}

func TestConvertDnsToDb_FlatMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_dns_to_db.json",
	})
	require.NoError(t, err)

	// State is always flat
	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", nil, nil, nil, "")
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

	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", nil, nil, nil, "")
	require.NoError(t, err)

	_, _, components, custom := classifyResources(t, data)
	require.Len(t, components, 0, "no components in flat state")
	require.Len(t, custom, 3, "expected 3 resources in zoo module")
}

func TestConvertMultiResourceModule_WithHCL(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_multi_resource_module.json",
	})
	require.NoError(t, err)

	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", nil, nil, nil, "testdata/tf_multi_resource_module")
	require.NoError(t, err)

	_, _, components, _ := classifyResources(t, data)
	require.Len(t, components, 0, "no components in flat state")

	// ComponentMapData should have zoo module with inputs/outputs
	require.NotNil(t, data.ComponentMapData)
	require.Len(t, data.ComponentMapData.Components, 1)
	zoo := data.ComponentMapData.Components[0]
	require.NotNil(t, zoo.Inputs, "zoo component should have inputs")
	require.Equal(t, resource.NewStringProperty("myprefix"), zoo.Inputs[resource.PropertyKey("prefix")])
	require.NotNil(t, zoo.Outputs, "zoo component should have outputs")
	_, hasAnimalName := zoo.Outputs[resource.PropertyKey("animal_name")]
	require.True(t, hasAnimalName, "outputs should have animal_name key")
	_, hasTag := zoo.Outputs[resource.PropertyKey("tag")]
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

	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", nil, nil, nil, "")
	require.NoError(t, err)

	_, _, components, _ := classifyResources(t, data)
	require.Len(t, components, 0, "no components in flat state")
}

func TestConvertDeepNestedMixed_FlatMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_deep_nested_mixed.json",
	})
	require.NoError(t, err)

	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", nil, nil, nil, "")
	require.NoError(t, err)

	_, _, components, _ := classifyResources(t, data)
	require.Len(t, components, 0, "state should produce 0 components")

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
		require.Equal(t, stackURN, string(r.Parent), "resource %s should be parented to Stack", r.URN)
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

	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", nil, nil, nil, "testdata/tf_complex_expressions")
	require.NoError(t, err)

	_, _, components, _ := classifyResources(t, data)
	require.Len(t, components, 0, "no components in flat state")

	require.NotNil(t, data.ComponentMapData)
	// Find svc-0 and svc-1 in ComponentMapData.Components by name
	var svc0, svc1 *PulumiResource
	for i, c := range data.ComponentMapData.Components {
		if c.Name == "svc-0" {
			svc0 = &data.ComponentMapData.Components[i]
		}
		if c.Name == "svc-1" {
			svc1 = &data.ComponentMapData.Components[i]
		}
	}
	require.NotNil(t, svc0, "svc-0 should exist in component map data")
	require.NotNil(t, svc1, "svc-1 should exist in component map data")

	require.NotNil(t, svc0.Inputs)
	require.Equal(t, resource.NewStringProperty("svc-00"), svc0.Inputs[resource.PropertyKey("prefix")])
	require.Equal(t, resource.NewBoolProperty(true), svc0.Inputs[resource.PropertyKey("is_primary")])
	require.Equal(t, resource.NewStringProperty("SERVICE-0"), svc0.Inputs[resource.PropertyKey("label")])

	require.NotNil(t, svc1.Inputs)
	require.Equal(t, resource.NewStringProperty("svc-01"), svc1.Inputs[resource.PropertyKey("prefix")])
	require.Equal(t, resource.NewBoolProperty(false), svc1.Inputs[resource.PropertyKey("is_primary")])
	require.Equal(t, resource.NewStringProperty("SERVICE-1"), svc1.Inputs[resource.PropertyKey("label")])
}

// --- Tfvars resolution (Fixture 5) ---

func TestConvertTfvarsResolution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_tfvars_resolution.json",
	})
	require.NoError(t, err)

	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", nil, nil, nil, "testdata/tf_tfvars_resolution")
	require.NoError(t, err)

	_, _, components, _ := classifyResources(t, data)
	require.Len(t, components, 0, "no components in flat state")

	require.NotNil(t, data.ComponentMapData)
	require.GreaterOrEqual(t, len(data.ComponentMapData.Components), 1)
	comp := data.ComponentMapData.Components[0]
	require.NotNil(t, comp.Inputs)
	require.Equal(t, resource.NewStringProperty("staging"), comp.Inputs[resource.PropertyKey("prefix")])
	_, hasSuffix := comp.Inputs[resource.PropertyKey("suffix")]
	require.False(t, hasSuffix, "variable defaults should not be in inputs")
}

// --- Special key sanitization (Fixture 6) ---

func TestConvertSpecialKeyModules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_special_key_modules.json",
	})
	require.NoError(t, err)

	data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", nil, nil, nil, "")
	require.NoError(t, err)

	_, _, components, _ := classifyResources(t, data)
	require.Len(t, components, 0, "no components in flat state")
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

			data, err := TranslateState(ctx, tfState, nil, "dev", "test-project", nil, nil, nil, "")
			require.NoError(t, err)

			_, _, components, _ := classifyResources(t, data)
			require.Len(t, components, 0, "flat mode should produce 0 components for %s", tt.name)
		})
	}
}
