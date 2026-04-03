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

package hcl

import (
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func parseExpr(t *testing.T, src string) hcl.Expression {
	t.Helper()
	expr, diags := hclsyntax.ParseExpression([]byte(src), "test.hcl", hcl.Pos{})
	require.False(t, diags.HasErrors(), diags.Error())
	return expr
}

func TestEvaluateLiteral(t *testing.T) {
	t.Parallel()
	ctx := NewEvalContext(nil, nil, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, `"10.0.0.0/16"`))
	require.NoError(t, err)
	require.Equal(t, "10.0.0.0/16", val.AsString())
}

func TestEvaluateVariableRef(t *testing.T) {
	t.Parallel()
	vars := map[string]cty.Value{"cidr": cty.StringVal("10.0.0.0/16")}
	ctx := NewEvalContext(vars, nil, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, "var.cidr"))
	require.NoError(t, err)
	require.Equal(t, "10.0.0.0/16", val.AsString())
}

func TestEvaluateResourceRef(t *testing.T) {
	t.Parallel()
	resources := map[string]map[string]cty.Value{
		"random_pet": {"this": cty.ObjectVal(map[string]cty.Value{
			"id":        cty.StringVal("test-0-creative-doberman"),
			"prefix":    cty.StringVal("test-0"),
			"separator": cty.StringVal("-"),
		})},
	}
	ctx := NewEvalContext(nil, resources, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, "random_pet.this.id"))
	require.NoError(t, err)
	require.Equal(t, "test-0-creative-doberman", val.AsString())
}

func TestEvaluateModuleOutputRef(t *testing.T) {
	t.Parallel()
	moduleOutputs := map[string]map[string]cty.Value{
		"pet": {"name": cty.StringVal("test-0-creative-doberman")},
	}
	ctx := NewEvalContext(nil, nil, moduleOutputs)
	val, err := ctx.EvaluateExpression(parseExpr(t, "module.pet.name"))
	require.NoError(t, err)
	require.Equal(t, "test-0-creative-doberman", val.AsString())
}

func TestEvaluateFunction_Join(t *testing.T) {
	t.Parallel()
	ctx := NewEvalContext(nil, nil, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, `join("-", ["a", "b", "c"])`))
	require.NoError(t, err)
	require.Equal(t, "a-b-c", val.AsString())
}

func TestEvaluateFunction_Upper(t *testing.T) {
	t.Parallel()
	ctx := NewEvalContext(nil, nil, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, `upper("hello")`))
	require.NoError(t, err)
	require.Equal(t, "HELLO", val.AsString())
}

func TestEvaluateConditional(t *testing.T) {
	t.Parallel()
	vars := map[string]cty.Value{"enable": cty.True}
	ctx := NewEvalContext(vars, nil, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, `var.enable ? "yes" : "no"`))
	require.NoError(t, err)
	require.Equal(t, "yes", val.AsString())
}

func TestEvaluateForExpression(t *testing.T) {
	t.Parallel()
	vars := map[string]cty.Value{
		"names": cty.ListVal([]cty.Value{cty.StringVal("a"), cty.StringVal("b")}),
	}
	ctx := NewEvalContext(vars, nil, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, `[for s in var.names : upper(s)]`))
	require.NoError(t, err)
	require.Equal(t, 2, val.LengthInt())
}

func TestEvaluateUnsupportedFunction(t *testing.T) {
	t.Parallel()
	ctx := NewEvalContext(nil, nil, nil)
	_, err := ctx.EvaluateExpression(parseExpr(t, `totally_fake_function("x")`))
	require.Error(t, err)
}

func TestEvaluateStringInterpolation(t *testing.T) {
	t.Parallel()
	vars := map[string]cty.Value{"prefix": cty.StringVal("test")}
	ctx := NewEvalContext(vars, nil, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, `"${var.prefix}-0"`))
	require.NoError(t, err)
	require.Equal(t, "test-0", val.AsString())
}
