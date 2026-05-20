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

	"github.com/pulumi/opentofu/addrs"
	"github.com/pulumi/opentofu/checks"
	"github.com/pulumi/opentofu/configs"
	"github.com/pulumi/opentofu/instances"
	"github.com/pulumi/opentofu/lang"
	"github.com/pulumi/opentofu/plans"
	"github.com/pulumi/opentofu/states"
	"github.com/pulumi/opentofu/tfdiags"
	"github.com/pulumi/opentofu/tofu"
)

// EvalScopes builds and walks the OpenTofu eval graph once, then provides
// scopes for any module instance address on demand. This avoids the cost of
// rebuilding the graph for each module (which is what tofuCtx.Eval does).
type EvalScopes struct {
	walker *tofu.ContextGraphWalker
	diags  tfdiags.Diagnostics
}

// BuildEvalScopes builds the eval graph and walks it once. The returned
// EvalScopes can then be used to get a *lang.Scope for any module instance.
func BuildEvalScopes(
	ctx context.Context,
	tofuCtx *tofu.Context,
	config *configs.Config,
	state *states.State,
	rootVars tofu.InputValues,
) (*EvalScopes, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics

	// Deep copy state so we don't affect the caller's copy.
	state = state.DeepCopy()

	providerFunctionTracker := make(tofu.ProviderFunctionMapping)

	graph, moreDiags := (&tofu.EvalGraphBuilder{
		Config:                  config,
		State:                   state,
		RootVariableValues:      rootVars,
		Plugins:                 tofuCtx.ContextPlugins(),
		ProviderFunctionTracker: providerFunctionTracker,
	}).Build(addrs.RootModuleInstance)
	diags = diags.Append(moreDiags)
	if moreDiags.HasErrors() {
		return nil, diags
	}

	// Construct the graph walker using exported fields.
	walker := &tofu.ContextGraphWalker{
		Context:                 tofuCtx,
		State:                   state.SyncWrapper(),
		Config:                  config,
		Changes:                 plans.NewChanges().SyncWrapper(),
		Checks:                  checks.NewState(config),
		InstanceExpander:        instances.NewExpander(),
		ImportResolver:          tofu.NewImportResolver(),
		Operation:               tofu.WalkEval,
		ProviderFunctionTracker: providerFunctionTracker,
	}

	// Walk the graph once — this evaluates all variables, locals, outputs.
	walkDiags := graph.Walk(ctx, walker)
	diags = diags.Append(walkDiags)
	diags = diags.Append(walker.NonFatalDiagnostics)

	return &EvalScopes{walker: walker, diags: diags}, diags
}

// Scope returns a *lang.Scope for the given module instance address.
// The walker caches eval contexts, so this is a cheap lookup.
func (es *EvalScopes) Scope(addr addrs.ModuleInstance) *lang.Scope {
	evalCtx := es.walker.EnterPath(addr)
	return evalCtx.EvaluationScope(nil, nil, tofu.EvalDataForNoInstanceKey)
}

// Diagnostics returns the diagnostics from the graph build and walk.
func (es *EvalScopes) Diagnostics() tfdiags.Diagnostics {
	return es.diags
}
