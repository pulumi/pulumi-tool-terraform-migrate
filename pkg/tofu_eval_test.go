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
	"path/filepath"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/pulumi/opentofu/addrs"
	"github.com/pulumi/opentofu/lang"
	"github.com/pulumi/opentofu/tofu"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func TestDetectStateFormat_RawTfstate(t *testing.T) {
	format, err := DetectStateFormat(filepath.Join("testdata", "tofu_tfstate_indexed_modules.tfstate"))
	require.NoError(t, err)
	assert.Equal(t, StateFormatRaw, format)
}

func TestDetectStateFormat_TofuShowJson(t *testing.T) {
	format, err := DetectStateFormat(filepath.Join("testdata", "tofu_state_indexed_modules.json"))
	require.NoError(t, err)
	assert.Equal(t, StateFormatTofuShowJSON, format)
}

func TestLoadConfig(t *testing.T) {
	tfDir, err := filepath.Abs(filepath.Join("testdata", "tf_indexed_modules"))
	require.NoError(t, err)

	config, err := LoadConfig(tfDir)
	require.NoError(t, err)
	require.NotNil(t, config)

	// Root module should have a "pet" module call.
	_, hasPet := config.Root.Module.ModuleCalls["pet"]
	assert.True(t, hasPet, "expected root module to have a 'pet' module call")

	// Child module "pet" should have a "prefix" variable.
	petChild, hasPetChild := config.Children["pet"]
	require.True(t, hasPetChild, "expected config to have a 'pet' child")
	_, hasPrefix := petChild.Module.Variables["prefix"]
	assert.True(t, hasPrefix, "expected 'pet' child module to have a 'prefix' variable")
}

func TestLoadRawState(t *testing.T) {
	state, err := LoadRawState(filepath.Join("testdata", "tofu_tfstate_indexed_modules.tfstate"))
	require.NoError(t, err)
	require.NotNil(t, state)

	// Should have resources from module.pet[0] and module.pet[1].
	mod0 := state.Module(addrs.RootModuleInstance.Child("pet", addrs.IntKey(0)))
	require.NotNil(t, mod0, "expected module.pet[0] to exist in state")
	assert.Greater(t, len(mod0.Resources), 0, "expected module.pet[0] to have resources")

	mod1 := state.Module(addrs.RootModuleInstance.Child("pet", addrs.IntKey(1)))
	require.NotNil(t, mod1, "expected module.pet[1] to exist in state")
	assert.Greater(t, len(mod1.Resources), 0, "expected module.pet[1] to have resources")
}

func TestLoadProviders(t *testing.T) {
	tfDir, err := filepath.Abs(filepath.Join("testdata", "tf_indexed_modules"))
	require.NoError(t, err)

	providerDir := filepath.Join(tfDir, ".terraform", "providers")
	if _, err := os.Stat(providerDir); os.IsNotExist(err) {
		t.Skip("skipping: .terraform/providers not found")
	}

	config, err := LoadConfig(tfDir)
	require.NoError(t, err)

	factories, err := LoadProviders(config, tfDir)
	require.NoError(t, err)
	assert.Greater(t, len(factories), 0, "expected at least one provider factory")
}

func TestEvaluate_IndexedModules(t *testing.T) {
	tfDir, err := filepath.Abs(filepath.Join("testdata", "tf_indexed_modules"))
	require.NoError(t, err)

	providerDir := filepath.Join(tfDir, ".terraform", "providers")
	if _, err := os.Stat(providerDir); os.IsNotExist(err) {
		t.Skip("skipping: .terraform/providers not found")
	}

	config, err := LoadConfig(tfDir)
	require.NoError(t, err)

	state, err := LoadRawState(filepath.Join(tfDir, "terraform.tfstate"))
	require.NoError(t, err)

	tofuCtx, err := Evaluate(config, state, tfDir)
	require.NoError(t, err)
	require.NotNil(t, tofuCtx)

	ctx := context.Background()

	// Evaluate var.prefix in module.pet[0] — should be "test-0".
	child0Addr := addrs.RootModuleInstance.Child("pet", addrs.IntKey(0))
	scope0, diags := tofuCtx.Eval(ctx, config, state, child0Addr, &tofu.EvalOpts{})
	require.False(t, diags.HasErrors(), "Eval diags for pet[0]: %s", diags.Err())
	require.NotNil(t, scope0)

	val0 := evalExpr(t, scope0, "var.prefix")
	assert.Equal(t, "test-0", val0.AsString())

	// Evaluate var.prefix in module.pet[1] — should be "test-1".
	child1Addr := addrs.RootModuleInstance.Child("pet", addrs.IntKey(1))
	scope1, diags := tofuCtx.Eval(ctx, config, state, child1Addr, &tofu.EvalOpts{})
	require.False(t, diags.HasErrors(), "Eval diags for pet[1]: %s", diags.Err())
	require.NotNil(t, scope1)

	val1 := evalExpr(t, scope1, "var.prefix")
	assert.Equal(t, "test-1", val1.AsString())
}

// evalExpr is a test helper that parses an HCL expression and evaluates it
// against the given scope, returning the resulting cty.Value.
func evalExpr(t *testing.T, scope *lang.Scope, expr string) cty.Value {
	t.Helper()

	parsed, diags := hclsyntax.ParseExpression([]byte(expr), "test", hcl.Pos{Line: 1, Column: 1})
	require.False(t, diags.HasErrors(), "parse expression %q: %s", expr, diags.Error())

	val, evalDiags := scope.EvalExpr(parsed, cty.DynamicPseudoType)
	require.False(t, evalDiags.HasErrors(), "eval expr %q: %s", expr, evalDiags.Err())

	return val
}
