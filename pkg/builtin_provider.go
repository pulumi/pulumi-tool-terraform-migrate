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
	"github.com/pulumi/opentofu/configs/configschema"
	"github.com/pulumi/opentofu/providers"
	"github.com/zclconf/go-cty/cty"
)

// builtinTerraformProvider is a minimal stub implementation of the built-in
// "terraform" provider (terraform.io/builtin/terraform). It provides only
// the schema for terraform_remote_state and terraform_data so that
// tofuCtx.Eval() can build its graph without needing the real provider
// (which pulls in all remote backend dependencies).
type builtinTerraformProvider struct{}

func (p *builtinTerraformProvider) GetProviderSchema() providers.GetProviderSchemaResponse {
	return providers.GetProviderSchemaResponse{
		DataSources: map[string]providers.Schema{
			"terraform_remote_state": {
				Block: &configschema.Block{
					Attributes: map[string]*configschema.Attribute{
						"backend":   {Type: cty.String, Required: true},
						"config":    {Type: cty.DynamicPseudoType, Optional: true},
						"defaults":  {Type: cty.DynamicPseudoType, Optional: true},
						"outputs":   {Type: cty.DynamicPseudoType, Computed: true},
						"workspace": {Type: cty.String, Optional: true},
					},
				},
			},
		},
		ResourceTypes: map[string]providers.Schema{
			"terraform_data": {
				Block: &configschema.Block{
					Attributes: map[string]*configschema.Attribute{
						"input":            {Type: cty.DynamicPseudoType, Optional: true},
						"output":           {Type: cty.DynamicPseudoType, Computed: true},
						"triggers_replace": {Type: cty.DynamicPseudoType, Optional: true},
						"id":               {Type: cty.String, Computed: true},
					},
				},
			},
		},
	}
}

func (p *builtinTerraformProvider) ValidateProviderConfig(req providers.ValidateProviderConfigRequest) providers.ValidateProviderConfigResponse {
	return providers.ValidateProviderConfigResponse{PreparedConfig: req.Config}
}

func (p *builtinTerraformProvider) ValidateResourceConfig(req providers.ValidateResourceConfigRequest) providers.ValidateResourceConfigResponse {
	return providers.ValidateResourceConfigResponse{}
}

func (p *builtinTerraformProvider) ValidateDataResourceConfig(req providers.ValidateDataResourceConfigRequest) providers.ValidateDataResourceConfigResponse {
	return providers.ValidateDataResourceConfigResponse{}
}

func (p *builtinTerraformProvider) ConfigureProvider(providers.ConfigureProviderRequest) providers.ConfigureProviderResponse {
	return providers.ConfigureProviderResponse{}
}

func (p *builtinTerraformProvider) UpgradeResourceState(req providers.UpgradeResourceStateRequest) providers.UpgradeResourceStateResponse {
	return providers.UpgradeResourceStateResponse{}
}

func (p *builtinTerraformProvider) ReadResource(req providers.ReadResourceRequest) providers.ReadResourceResponse {
	return providers.ReadResourceResponse{NewState: req.PriorState}
}

func (p *builtinTerraformProvider) ReadDataSource(req providers.ReadDataSourceRequest) providers.ReadDataSourceResponse {
	return providers.ReadDataSourceResponse{State: req.Config}
}

func (p *builtinTerraformProvider) PlanResourceChange(req providers.PlanResourceChangeRequest) providers.PlanResourceChangeResponse {
	return providers.PlanResourceChangeResponse{PlannedState: req.ProposedNewState}
}

func (p *builtinTerraformProvider) ApplyResourceChange(req providers.ApplyResourceChangeRequest) providers.ApplyResourceChangeResponse {
	return providers.ApplyResourceChangeResponse{NewState: req.PlannedState}
}

func (p *builtinTerraformProvider) ImportResourceState(req providers.ImportResourceStateRequest) providers.ImportResourceStateResponse {
	return providers.ImportResourceStateResponse{}
}

func (p *builtinTerraformProvider) MoveResourceState(req providers.MoveResourceStateRequest) providers.MoveResourceStateResponse {
	return providers.MoveResourceStateResponse{}
}

func (p *builtinTerraformProvider) GetFunctions() providers.GetFunctionsResponse {
	return providers.GetFunctionsResponse{}
}

func (p *builtinTerraformProvider) CallFunction(req providers.CallFunctionRequest) providers.CallFunctionResponse {
	return providers.CallFunctionResponse{}
}

func (p *builtinTerraformProvider) Stop() error { return nil }
func (p *builtinTerraformProvider) Close() error { return nil }
