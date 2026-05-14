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
	"fmt"
	"os"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/pulumi/opentofu/addrs"
	"github.com/pulumi/opentofu/configs"
	"github.com/pulumi/opentofu/states"
	"github.com/pulumi/opentofu/tofu"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/bridge"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/providermap"
	"github.com/zclconf/go-cty/cty"
)

// ModuleMap is the top-level structure for the module-map.json sidecar file.
type ModuleMap struct {
	Modules       map[string]*ModuleMapEntry `json:"modules"`
	RootResources []ModuleResource           `json:"rootResources,omitempty"`
}

// ModuleResource represents a single resource within a module instance.
type ModuleResource struct {
	Mode             string                 `json:"mode"` // "managed" or "data"
	TranslatedURN    string                 `json:"translatedUrn"`
	TerraformAddress string                 `json:"terraformAddress"`
	ImportID         string                 `json:"importId"`
	Attributes       map[string]interface{} `json:"attributes,omitempty"`
}

// ModuleMapEntry represents a single module instance in the module map.
type ModuleMapEntry struct {
	TerraformPath string                     `json:"terraformPath"`
	Source        string                     `json:"source,omitempty"`
	IndexKey      string                     `json:"indexKey,omitempty"`
	IndexType     string                     `json:"indexType,omitempty"`
	Resources     []ModuleResource           `json:"resources"`
	Interface     *ModuleInterface           `json:"interface,omitempty"`
	Modules       map[string]*ModuleMapEntry `json:"modules,omitempty"`
}

// ModuleInterface describes the inputs and outputs of a module.
type ModuleInterface struct {
	Inputs  []ModuleInterfaceField `json:"inputs"`
	Outputs []ModuleInterfaceField `json:"outputs"`
}

// ModuleInterfaceField describes a single input variable or output value.
type ModuleInterfaceField struct {
	Name           string      `json:"name"`
	Type           interface{} `json:"type,omitempty"`
	Required       bool        `json:"required,omitempty"`
	Default        interface{} `json:"default,omitempty"`
	Description    string      `json:"description,omitempty"`
	Expression     string      `json:"expression,omitempty"`
	EvaluatedValue interface{} `json:"evaluatedValue,omitempty"`
}

// BuildModuleMap constructs a ModuleMap from Terraform configuration and state.
// tofuCtx and state may be nil if evaluation is not available.
// pulumiProviders may be nil if URN generation should fall back to raw addresses.
func BuildModuleMap(
	config *configs.Config,
	tofuCtx *tofu.Context,
	state *states.State,
	pulumiProviders map[providermap.TerraformProviderName]*ProviderWithMetadata,
	sensitivityMap SensitivityMap,
	stackName string,
	projectName string,
) (*ModuleMap, error) {
	mm := &ModuleMap{
		Modules: make(map[string]*ModuleMapEntry),
	}

	err := buildModuleMapLevel(mm.Modules, config, tofuCtx, state, pulumiProviders, sensitivityMap, stackName, projectName, nil) //nolint:lll
	if err != nil {
		return nil, err
	}

	// Collect root-level resources (empty segments = root module).
	rootResources := matchResources(state, nil, pulumiProviders, sensitivityMap, stackName, projectName)
	if len(rootResources) > 0 {
		mm.RootResources = rootResources
	}

	return mm, nil
}

// buildModuleMapLevel processes one level of module calls and recurses into children.
// parentSegments tracks the module path prefix for nested modules.
func buildModuleMapLevel(
	target map[string]*ModuleMapEntry,
	config *configs.Config,
	tofuCtx *tofu.Context,
	state *states.State,
	pulumiProviders map[providermap.TerraformProviderName]*ProviderWithMetadata,
	sensitivityMap SensitivityMap,
	stackName string,
	projectName string,
	parentSegments []moduleSegment,
) error {
	if config == nil || config.Module == nil {
		return nil
	}

	for name, call := range config.Module.ModuleCalls {
		// Discover instances of this module from state.
		instances := discoverModuleInstances(state, parentSegments, name)

		// Get call-site expression text for each attribute.
		callExpressions := getCallExpressions(call)

		for _, inst := range instances {
			segments := make([]moduleSegment, len(parentSegments)+1)
			copy(segments, parentSegments)
			segments[len(parentSegments)] = moduleSegment{name: name, key: inst.key}

			mapKey := name
			if inst.key != "" {
				mapKey = name + "[" + formatKey(inst.key) + "]"
			}

			entry := &ModuleMapEntry{
				TerraformPath: buildModulePath(segments),
				Source:        call.SourceAddrRaw,
				IndexKey:      inst.key,
				Resources:     matchResources(state, segments, pulumiProviders, sensitivityMap, stackName, projectName),
			}

			// Determine index type.
			if inst.key != "" {
				if _, err := fmt.Sscanf(inst.key, "%d", new(int)); err == nil {
					entry.IndexType = "int"
				} else {
					entry.IndexType = "string"
				}
			}

			// Build interface from child config.
			childConfig := config.Children[name]
			if childConfig != nil && childConfig.Module != nil {
				entry.Interface = buildModuleInterface(childConfig, callExpressions)

				// If eval is available, populate evaluatedValue for inputs.
				if tofuCtx != nil && state != nil {
					populateEvaluatedValues(entry.Interface, tofuCtx, config, state, segments)
				}
			}

			// Recurse into nested modules.
			if childConfig != nil && len(childConfig.Module.ModuleCalls) > 0 {
				entry.Modules = make(map[string]*ModuleMapEntry)
				err := buildModuleMapLevel(
					entry.Modules, childConfig, tofuCtx, state,
					pulumiProviders, sensitivityMap, stackName, projectName, segments,
				)
				if err != nil {
					return err
				}
				if len(entry.Modules) == 0 {
					entry.Modules = nil
				}
			}

			target[mapKey] = entry
		}
	}

	return nil
}

// moduleInstance represents a discovered module instance from state.
type moduleInstance struct {
	key string // empty for non-indexed, "0"/"1" for count, "key" for for_each
}

// discoverModuleInstances finds unique module instances from raw state that match
// the given parent path and module name.
func discoverModuleInstances(state *states.State, parentSegments []moduleSegment, moduleName string) []moduleInstance {
	seen := map[string]bool{}
	var instances []moduleInstance

	parentDepth := len(parentSegments)

	if state != nil {
		for _, module := range state.Modules {
			segments := moduleSegmentsFromAddr(module.Addr)
			if len(segments) <= parentDepth {
				continue
			}

			// Check that parent path matches.
			match := true
			for i, ps := range parentSegments {
				if segments[i].name != ps.name || segments[i].key != ps.key {
					match = false
					break
				}
			}
			if !match {
				continue
			}

			// Check that the next segment matches our module name.
			seg := segments[parentDepth]
			if seg.name != moduleName {
				continue
			}

			instanceKey := seg.key
			if !seen[instanceKey] {
				seen[instanceKey] = true
				instances = append(instances, moduleInstance{key: instanceKey})
			}
		}
	}

	// Sort instances for deterministic output.
	sort.Slice(instances, func(i, j int) bool {
		return instances[i].key < instances[j].key
	})

	// If no instances found in state, still emit one entry for non-indexed modules
	// if they exist in config (they might just have no resources).
	if len(instances) == 0 {
		instances = append(instances, moduleInstance{key: ""})
	}

	return instances
}

// matchResources finds resources in raw state that belong to the given module instance
// and returns ModuleResource entries with URN, Terraform address, and import ID.
func matchResources(
	state *states.State,
	segments []moduleSegment,
	pulumiProviders map[providermap.TerraformProviderName]*ProviderWithMetadata,
	sensitivityMap SensitivityMap,
	stackName string,
	projectName string,
) []ModuleResource {
	var resources []ModuleResource
	modulePath := buildModulePath(segments)

	if state != nil {
		for _, module := range state.Modules {
			modSegments := moduleSegmentsFromAddr(module.Addr)
			if buildModulePath(modSegments) != modulePath {
				continue
			}

			for _, res := range module.Resources {
				providerName := res.ProviderConfig.Provider.String()
				resourceType := res.Addr.Resource.Type

				for instKey, inst := range res.Instances {
					if inst.Current == nil {
						continue
					}

					// Build the full address: module path + resource address + instance key
					address := res.Addr.Resource.String()
					if instKey != nil {
						address += instKey.String()
					}
					if len(module.Addr) > 0 {
						address = module.Addr.String() + "." + address
					}

					// Parse attributes from AttrsJSON.
					var attrs map[string]interface{}
					importID := ""
					if inst.Current.AttrsJSON != nil {
						if err := json.Unmarshal(inst.Current.AttrsJSON, &attrs); err == nil {
							if id, ok := attrs["id"]; ok {
								importID = fmt.Sprintf("%v", id)
							}
						}
					}

					// Determine mode string.
					mode := "managed"
					if res.Addr.Resource.Mode == addrs.DataResourceMode {
						mode = "data"
					}

					// Data sources don't map to Pulumi resources.
					urn := ""
					if mode == "managed" {
						urn = buildResourceURN(address, providerName, resourceType, pulumiProviders, stackName, projectName)
					}

					mr := ModuleResource{
						Mode:             mode,
						TranslatedURN:    urn,
						TerraformAddress: address,
						ImportID:         importID,
					}

					// Include attributes for all resources.
					// Data sources: full attributes. Managed resources: redact sensitive.
					if attrs != nil {
						if mode == "data" {
							mr.Attributes = attrs
						} else if sensitivityMap != nil {
							mr.Attributes = RedactSensitiveAttributes(attrs, sensitivityMap[resourceType])
						} else {
							mr.Attributes = attrs
						}
					}

					resources = append(resources, mr)
				}
			}
		}
	}

	if resources == nil {
		resources = []ModuleResource{}
	}
	return resources
}

// buildResourceURN constructs a Pulumi URN for a Terraform resource, or falls back
// to the raw Terraform address if provider mapping is unavailable.
func buildResourceURN(
	address string,
	providerName string,
	resourceType string,
	pulumiProviders map[providermap.TerraformProviderName]*ProviderWithMetadata,
	stackName string,
	projectName string,
) string {
	if pulumiProviders == nil {
		return address
	}

	prov, ok := pulumiProviders[providermap.TerraformProviderName(providerName)]
	if !ok {
		return address
	}

	typeToken, err := bridge.PulumiTypeToken(resourceType, prov.Provider)
	if err != nil {
		return address
	}

	pulumiName := PulumiNameFromTerraformAddress(address, resourceType)
	return fmt.Sprintf("urn:pulumi:%s::%s::%s::%s", stackName, projectName, typeToken, pulumiName)
}

// getCallExpressions extracts the raw HCL expression text for each attribute
// in a module call's config body.
func getCallExpressions(call *configs.ModuleCall) map[string]string {
	result := make(map[string]string)
	if call.Config == nil {
		return result
	}

	attrs, _ := call.Config.JustAttributes()
	for attrName, attr := range attrs {
		rng := attr.Expr.Range()
		src, err := os.ReadFile(rng.Filename)
		if err != nil {
			continue
		}
		startByte := rng.Start.Byte
		endByte := rng.End.Byte
		if startByte >= 0 && endByte <= len(src) && startByte < endByte {
			result[attrName] = string(src[startByte:endByte])
		}
	}

	return result
}

// buildModuleInterface constructs a ModuleInterface from a child config's
// variables and outputs.
func buildModuleInterface(childConfig *configs.Config, callExpressions map[string]string) *ModuleInterface {
	iface := &ModuleInterface{}

	// Build inputs from variables.
	varNames := make([]string, 0, len(childConfig.Module.Variables))
	for name := range childConfig.Module.Variables {
		varNames = append(varNames, name)
	}
	sort.Strings(varNames)

	for _, varName := range varNames {
		v := childConfig.Module.Variables[varName]
		field := ModuleInterfaceField{
			Name:        varName,
			Description: v.Description,
		}

		// Type: convert cty.Type to a string representation.
		if v.Type != cty.NilType {
			field.Type = v.Type.FriendlyName()
		}

		// Required: a variable is required if it has no default value.
		if v.Default == cty.NilVal {
			field.Required = true
		} else {
			field.Default = ctyValueToInterface(v.Default)
		}

		// Expression from call site.
		if expr, ok := callExpressions[varName]; ok {
			field.Expression = expr
		}

		iface.Inputs = append(iface.Inputs, field)
	}

	// Build outputs.
	outputNames := make([]string, 0, len(childConfig.Module.Outputs))
	for name := range childConfig.Module.Outputs {
		outputNames = append(outputNames, name)
	}
	sort.Strings(outputNames)

	for _, outName := range outputNames {
		o := childConfig.Module.Outputs[outName]
		field := ModuleInterfaceField{
			Name:        outName,
			Description: o.Description,
		}
		iface.Outputs = append(iface.Outputs, field)
	}

	return iface
}

// populateEvaluatedValues uses tofuCtx.Eval() to evaluate variable values
// in a specific module instance and populates the EvaluatedValue field.
func populateEvaluatedValues(
	iface *ModuleInterface,
	tofuCtx *tofu.Context,
	config *configs.Config,
	state *states.State,
	segments []moduleSegment,
) {
	ctx := context.Background()

	// Build the module instance address from segments.
	addr := addrs.RootModuleInstance
	for _, seg := range segments {
		if seg.key == "" {
			addr = addr.Child(seg.name, addrs.NoKey)
		} else if _, err := fmt.Sscanf(seg.key, "%d", new(int)); err == nil {
			var idx int
			fmt.Sscanf(seg.key, "%d", &idx)
			addr = addr.Child(seg.name, addrs.IntKey(idx))
		} else {
			addr = addr.Child(seg.name, addrs.StringKey(seg.key))
		}
	}

	scope, diags := tofuCtx.Eval(ctx, config, state, addr, &tofu.EvalOpts{})
	if diags.HasErrors() || scope == nil {
		return
	}

	for i, input := range iface.Inputs {
		expr, parseDiags := hclsyntax.ParseExpression(
			[]byte("var."+input.Name), "<eval>", hcl.Pos{Line: 1, Column: 1},
		)
		if parseDiags.HasErrors() {
			continue
		}

		val, evalDiags := scope.EvalExpr(expr, cty.DynamicPseudoType)
		if evalDiags.HasErrors() {
			continue
		}

		iface.Inputs[i].EvaluatedValue = ctyValueToInterface(val)
	}
}

// ctyValueToInterface converts a cty.Value to a plain Go value suitable for JSON serialization.
func ctyValueToInterface(v cty.Value) interface{} {
	if v == cty.NilVal || !v.IsKnown() {
		return nil
	}
	if v.IsNull() {
		return nil
	}

	ty := v.Type()

	switch {
	case ty == cty.String:
		return v.AsString()
	case ty == cty.Number:
		bf := v.AsBigFloat()
		if bf.IsInt() {
			i, _ := bf.Int64()
			return i
		}
		f, _ := bf.Float64()
		return f
	case ty == cty.Bool:
		return v.True()
	case ty.IsListType() || ty.IsTupleType() || ty.IsSetType():
		var result []interface{}
		for it := v.ElementIterator(); it.Next(); {
			_, elem := it.Element()
			result = append(result, ctyValueToInterface(elem))
		}
		return result
	case ty.IsMapType() || ty.IsObjectType():
		result := make(map[string]interface{})
		for it := v.ElementIterator(); it.Next(); {
			key, elem := it.Element()
			result[key.AsString()] = ctyValueToInterface(elem)
		}
		return result
	default:
		return nil
	}
}

// WriteModuleMap serializes a ModuleMap to JSON and writes it to the given path.
func WriteModuleMap(mm *ModuleMap, path string) error {
	data, err := json.MarshalIndent(mm, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling module map: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing module map to %s: %w", path, err)
	}
	return nil
}
