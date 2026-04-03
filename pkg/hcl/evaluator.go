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
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/pulumi/opentofu/lang"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

// EvalContext wraps an HCL evaluation context populated with Terraform state data
// and the full Terraform function library.
type EvalContext struct {
	hclCtx *hcl.EvalContext
}

// NewEvalContext creates an HCL evaluation context.
//
// Parameters:
//   - variables: var.* namespace values (from tfvars or variable defaults)
//   - resources: resource type -> instance name -> attributes object (from TF state)
//   - moduleOutputs: module name -> output name -> value (from TF state)
func NewEvalContext(
	variables map[string]cty.Value,
	resources map[string]map[string]cty.Value,
	moduleOutputs map[string]map[string]cty.Value,
) *EvalContext {
	ctx := &hcl.EvalContext{
		Variables: map[string]cty.Value{},
		Functions: buildFunctionTable(),
	}

	if len(variables) > 0 {
		ctx.Variables["var"] = cty.ObjectVal(variables)
	}

	for resType, instances := range resources {
		ctx.Variables[resType] = cty.ObjectVal(instances)
	}

	if len(moduleOutputs) > 0 {
		modVals := map[string]cty.Value{}
		for modName, outputs := range moduleOutputs {
			modVals[modName] = cty.ObjectVal(outputs)
		}
		ctx.Variables["module"] = cty.ObjectVal(modVals)
	}

	return &EvalContext{hclCtx: ctx}
}

// AddVariables adds additional variables to the eval context.
func (e *EvalContext) AddVariables(vars map[string]cty.Value) {
	for k, v := range vars {
		e.hclCtx.Variables[k] = v
	}
}

// EvaluateExpression evaluates an HCL expression against the context.
func (e *EvalContext) EvaluateExpression(expr hcl.Expression) (cty.Value, error) {
	val, diags := expr.Value(e.hclCtx)
	if diags.HasErrors() {
		return cty.NilVal, fmt.Errorf("expression evaluation failed: %s", diags.Error())
	}
	return val, nil
}

// buildFunctionTable returns the full Terraform-compatible function table
// using opentofu/lang.Scope which provides all standard + Terraform-specific functions.
func buildFunctionTable() map[string]function.Function {
	scope := &lang.Scope{BaseDir: ".", ConsoleMode: true}
	return scope.Functions()
}
