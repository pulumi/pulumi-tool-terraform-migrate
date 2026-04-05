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
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	tfjson "github.com/hashicorp/terraform-json"
	hclpkg "github.com/pulumi/pulumi-tool-terraform-migrate/pkg/hcl"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tofu"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

// populateComponentsFromHCL populates component inputs and outputs by parsing HCL sources.
// For each component that has an HCL source path (via sourceOverrides or auto-discovery),
// it parses the module call site, evaluates argument expressions, and converts to PropertyMap.
//
// When populateInputs=false, component inputs are left empty and a ComponentSchemaMetadata
// is returned instead (for use as a sidecar file by the code generator).
//
// If a schema is provided (via schemaOverrides), the parsed interface is validated against it.
func populateComponentsFromHCL(
	components []PulumiResource,
	componentTree []*componentNode,
	sourceOverrides map[string]string,
	schemaOverrides map[string]string,
	tfSourceDir string,
	populateInputs bool,
	scopedAttrs scopedResourceAttrs,
	tfState *tfjson.State,
) (*ComponentSchemaMetadata, error) {
	if tfSourceDir == "" {
		return nil, nil
	}

	// Parse module call sites from the root TF source directory
	callSites, err := hclpkg.ParseModuleCallSites(tfSourceDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to parse module call sites from %s: %v\n", tfSourceDir, err)
		return nil, nil
	}

	// Load all tfvars (terraform.tfvars + *.auto.tfvars)
	tfvars, err := hclpkg.LoadAllTfvars(tfSourceDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load tfvars: %v\n", err)
		tfvars = map[string]cty.Value{}
	}

	// Resolve remote module sources from .terraform/modules/ cache
	cachedModuleSources, _ := hclpkg.ResolveModuleSourcesFromCache(tfSourceDir)

	// Build data source attr map for the "data" eval context variable
	dataSourceAttrs := buildDataSourceAttrMap(tfState)

	// Root-scoped resource attrs for input evaluation
	resourceAttrs := scopedAttrs.forModule("")

	// Build a lookup from module name to call site
	callSiteMap := map[string]*hclpkg.ModuleCallSite{}
	for i := range callSites {
		callSiteMap[callSites[i].Name] = &callSites[i]
	}

	// Pre-pass: resolve source paths and evaluate module outputs for cross-references.
	// This builds the module.* namespace so input expressions like module.vpc.vpc_id resolve.
	moduleOutputValues := map[string]map[string]cty.Value{}
	resolvedSources := map[string]string{}
	for _, comp := range components {
		node := findComponentNode(componentTree, comp.Name)
		if node == nil {
			continue
		}
		sourcePath := resolveModuleSourcePath(node, sourceOverrides, callSiteMap, tfSourceDir, cachedModuleSources)
		if sourcePath == "" {
			continue
		}
		resolvedSources["module."+node.name] = sourcePath

		outputs, err := hclpkg.ParseModuleOutputs(sourcePath)
		if err != nil || len(outputs) == 0 {
			continue
		}
		moduleResourceAttrs := scopedAttrs.forModule(node.modulePath)
		outputEvalCtx := hclpkg.NewEvalContext(nil, moduleResourceAttrs, nil)

		evaluateAndAddLocals(sourcePath, outputEvalCtx)

		evaluated := map[string]cty.Value{}
		for _, o := range outputs {
			if o.Expression != nil {
				val, evalErr := outputEvalCtx.EvaluateExpression(o.Expression)
				if evalErr == nil {
					evaluated[o.Name] = val
				}
			}
		}
		if len(evaluated) > 0 {
			moduleOutputValues[node.name] = evaluated
		}
	}

	// Collect parsed variables/outputs for metadata (when populateInputs=false)
	parsedVariables := map[string][]hclpkg.ModuleVariable{}
	parsedOutputs := map[string][]hclpkg.ModuleOutput{}

	// Process each component
	for i, comp := range components {
		node := findComponentNode(componentTree, comp.Name)
		if node == nil {
			continue
		}
		moduleName := node.name

		// Resolve HCL source path: override > local auto-discovery > module cache
		sourcePath := resolveModuleSourcePath(node, sourceOverrides, callSiteMap, tfSourceDir, cachedModuleSources)

		// Parse module variables (needed for both metadata and default merging)
		if sourcePath != "" {
			if vars, err := hclpkg.ParseModuleVariables(sourcePath); err == nil {
				parsedVariables[moduleName] = vars
			}
		}

		// Populate inputs from call site argument evaluation (only when populateInputs=true)
		callSite, hasCallSite := callSiteMap[moduleName]
		if populateInputs && hasCallSite && len(callSite.Arguments) > 0 {
			evalVars := map[string]cty.Value{}
			maps.Copy(evalVars, tfvars)

			var metaVars map[string]cty.Value
			if node.key != "" {
				metaVars = buildMetaArgContext(node.key)
			}

			evalCtx := hclpkg.NewEvalContext(evalVars, resourceAttrs, moduleOutputValues)
			if metaVars != nil {
				evalCtx.AddVariables(metaVars)
			}

			// Add path.* refs
			evalCtx.AddVariables(map[string]cty.Value{
				"path": cty.ObjectVal(map[string]cty.Value{
					"module": cty.StringVal(tfSourceDir),
					"root":   cty.StringVal(tfSourceDir),
					"cwd":    cty.StringVal(tfSourceDir),
				}),
			})

			// Add data.* refs
			if dataSourceAttrs != nil {
				evalCtx.AddVariables(map[string]cty.Value{
					"data": cty.ObjectVal(dataSourceAttrs),
				})
			}

			// Evaluate and add local.* refs
			evaluateAndAddLocals(tfSourceDir, evalCtx)

			inputs := resource.PropertyMap{}
			for argName, argExpr := range callSite.Arguments {
				val, err := evalCtx.EvaluateExpression(argExpr)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to evaluate argument %q for module.%s: %v\n", argName, moduleName, err)
					continue
				}
				inputs[resource.PropertyKey(argName)] = hclpkg.CtyValueToPulumiPropertyValue(val)
			}
			// Merge variable defaults for any variable not already in call-site args
			if vars, ok := parsedVariables[moduleName]; ok {
				for _, v := range vars {
					if _, alreadySet := inputs[resource.PropertyKey(v.Name)]; !alreadySet && v.Default != nil {
						inputs[resource.PropertyKey(v.Name)] = hclpkg.CtyValueToPulumiPropertyValue(*v.Default)
					}
				}
			}

			if len(inputs) > 0 {
				components[i].Inputs = inputs
			}
		}

		// Populate outputs by evaluating output expressions from HCL.
		// Module output values are NOT persisted in TF state v4 format, so we
		// evaluate the output `value` expressions using the module's child resource
		// attributes from state as the eval context.
		if sourcePath != "" {
			outputs, err := hclpkg.ParseModuleOutputs(sourcePath)
			if err == nil {
				parsedOutputs[moduleName] = outputs

				// Build module-scoped eval context: output expressions reference
				// child resources (e.g., random_pet.this.id), so we need resource
				// attrs scoped to this module's address.
				moduleResourceAttrs := scopedAttrs.forModule(node.modulePath)

				// Also include var.* from the module's inputs (for outputs that reference inputs)
				moduleVars := map[string]cty.Value{}
				if components[i].Inputs != nil {
					moduleVars = hclpkg.PulumiPropertyMapToCtyMap(components[i].Inputs)
				}

				outputEvalCtx := hclpkg.NewEvalContext(moduleVars, moduleResourceAttrs, nil)

				evaluateAndAddLocals(sourcePath, outputEvalCtx)

				outputMap := resource.PropertyMap{}
				for _, o := range outputs {
					if o.Expression != nil {
						val, evalErr := outputEvalCtx.EvaluateExpression(o.Expression)
						if evalErr == nil {
							outputMap[resource.PropertyKey(o.Name)] = hclpkg.CtyValueToPulumiPropertyValue(val)
							continue
						}
						fmt.Fprintf(os.Stderr, "Warning: failed to evaluate output %q for module.%s: %v\n", o.Name, moduleName, evalErr)
					}
					// Fallback: record output name with empty value
					outputMap[resource.PropertyKey(o.Name)] = resource.NewStringProperty("")
				}
				if len(outputMap) > 0 {
					components[i].Outputs = outputMap
				}
			}
		}

		// Schema validation (when schema-path is provided)
		if schemaPath, ok := schemaOverrides["module."+moduleName]; ok {
			componentType := comp.Type
			schemaIface, err := LoadComponentSchema(schemaPath, componentType)
			if err != nil {
				return nil, fmt.Errorf("loading schema for module.%s: %w", moduleName, err)
			}

			// Build parsed interface from component's inputs/outputs
			parsed := &ComponentInterface{}
			for k := range components[i].Inputs {
				parsed.Inputs = append(parsed.Inputs, ComponentField{Name: string(k)})
			}
			for k := range components[i].Outputs {
				parsed.Outputs = append(parsed.Outputs, ComponentField{Name: string(k)})
			}

			if err := ValidateAgainstSchema(parsed, schemaIface); err != nil {
				return nil, fmt.Errorf("schema validation failed for module.%s: %w", moduleName, err)
			}
		}
	}

	// Always build metadata — the code generation agent benefits from typed
	// component interfaces regardless of whether inputs are in the state.
	metadata := buildComponentSchemaMetadata(components, componentTree, parsedVariables, parsedOutputs, resolvedSources)
	return metadata, nil
}

// resolveModuleSourcePath resolves the HCL source path for a module component.
// Priority: override > local auto-discovery > .terraform/modules cache.
func resolveModuleSourcePath(
	node *componentNode,
	sourceOverrides map[string]string,
	callSiteMap map[string]*hclpkg.ModuleCallSite,
	tfSourceDir string,
	cachedModuleSources map[string]string,
) string {
	if override, ok := sourceOverrides["module."+node.name]; ok {
		return override
	}
	if callSite, ok := callSiteMap[node.name]; ok && hclpkg.IsLocalModuleSource(callSite.Source) {
		return filepath.Join(tfSourceDir, callSite.Source)
	}
	if cached, ok := cachedModuleSources[node.modulePath]; ok {
		return cached
	}
	return ""
}

// evaluateLocals evaluates local definitions against an eval context.
// Uses iterative evaluation since locals can reference other locals.
func evaluateLocals(locals []hclpkg.LocalDefinition, evalCtx *hclpkg.EvalContext) map[string]cty.Value {
	resolved := map[string]cty.Value{}
	remaining := locals

	for pass := 0; pass < 10 && len(remaining) > 0; pass++ {
		if len(resolved) > 0 {
			evalCtx.AddVariables(map[string]cty.Value{"local": cty.ObjectVal(resolved)})
		}
		var unresolved []hclpkg.LocalDefinition
		for _, l := range remaining {
			val, err := evalCtx.EvaluateExpression(l.Expression)
			if err == nil {
				resolved[l.Name] = val
			} else {
				unresolved = append(unresolved, l)
			}
		}
		if len(unresolved) == len(remaining) {
			break // no progress
		}
		remaining = unresolved
	}

	return resolved
}

// evaluateAndAddLocals parses locals from sourcePath, evaluates them against
// evalCtx, and adds the results as local.* variables in the eval context.
func evaluateAndAddLocals(sourcePath string, evalCtx *hclpkg.EvalContext) {
	defs, _ := hclpkg.ParseLocals(sourcePath)
	if len(defs) == 0 {
		return
	}
	vals := evaluateLocals(defs, evalCtx)
	if len(vals) == 0 {
		return
	}
	evalCtx.AddVariables(map[string]cty.Value{"local": cty.ObjectVal(vals)})
}

// buildDataSourceAttrMap builds a nested cty.Value for the "data" eval context variable.
// Returns data = { aws_ami = { amzlinux2 = { id = "...", ... } } }
func buildDataSourceAttrMap(tfState *tfjson.State) map[string]cty.Value {
	typeMap := map[string]map[string]cty.Value{}
	if tfState == nil {
		return nil
	}

	tofu.VisitResources(tfState, func(r *tfjson.StateResource) error {
		if r.Mode != tfjson.DataResourceMode || r.AttributeValues == nil {
			return nil
		}
		parts := strings.Split(r.Address, ".")
		if len(parts) < 2 {
			return nil
		}
		resType := parts[len(parts)-2]
		resName := parts[len(parts)-1]

		attrs := map[string]cty.Value{}
		for k, v := range r.AttributeValues {
			attrs[k] = interfaceToCty(v)
		}
		if _, ok := typeMap[resType]; !ok {
			typeMap[resType] = map[string]cty.Value{}
		}
		typeMap[resType][resName] = cty.ObjectVal(attrs)
		return nil
	}, &tofu.VisitOptions{IncludeDataSources: true})

	if len(typeMap) == 0 {
		return nil
	}
	result := map[string]cty.Value{}
	for typeName, instances := range typeMap {
		result[typeName] = cty.ObjectVal(instances)
	}
	return result
}

// scopedResourceAttrs is a module-scoped resource attribute map.
// Maps modulePath → resourceType → resourceName → cty.ObjectVal(attributes).
// Root module resources have modulePath "".
type scopedResourceAttrs map[string]map[string]map[string]cty.Value

// buildScopedResourceAttrMap builds a module-scoped resource attribute map from TF state.
func buildScopedResourceAttrMap(tfState *tfjson.State) scopedResourceAttrs {
	result := scopedResourceAttrs{}
	if tfState == nil {
		return result
	}

	tofu.VisitResources(tfState, func(r *tfjson.StateResource) error {
		if r.AttributeValues == nil {
			return nil
		}
		modulePath, resType, resName := parseResourceAddress(r.Address)
		attrs := map[string]cty.Value{}
		for k, v := range r.AttributeValues {
			attrs[k] = interfaceToCty(v)
		}
		if _, ok := result[modulePath]; !ok {
			result[modulePath] = map[string]map[string]cty.Value{}
		}
		if _, ok := result[modulePath][resType]; !ok {
			result[modulePath][resType] = map[string]cty.Value{}
		}
		result[modulePath][resType][resName] = cty.ObjectVal(attrs)
		return nil
	}, &tofu.VisitOptions{})

	return result
}

// forModule returns the resource attrs scoped to a specific module path.
// For for_each instances like "module.vpc[\"us-east-1\"]", also tries the
// base module name "module.vpc".
func (s scopedResourceAttrs) forModule(modulePath string) map[string]map[string]cty.Value {
	if s == nil {
		return nil
	}
	if scoped, ok := s[modulePath]; ok {
		return scoped
	}
	// Try stripping index/key for for_each instances
	if idx := strings.Index(modulePath, "["); idx > 0 {
		base := modulePath[:idx]
		if scoped, ok := s[base]; ok {
			return scoped
		}
	}
	return nil
}

// parseResourceAddress splits a TF resource address into module path, type, and name.
// "module.vpc.aws_vpc.this" → ("module.vpc", "aws_vpc", "this")
// "aws_s3_bucket.mybucket" → ("", "aws_s3_bucket", "mybucket")
func parseResourceAddress(address string) (modulePath, resType, resName string) {
	parts := strings.Split(address, ".")
	if len(parts) < 2 {
		return "", address, ""
	}
	resName = parts[len(parts)-1]
	resType = parts[len(parts)-2]
	if len(parts) > 2 {
		modulePath = strings.Join(parts[:len(parts)-2], ".")
	}
	return
}


// interfaceToCty converts a Go interface{} (from JSON state) to a cty.Value.
func interfaceToCty(v interface{}) cty.Value {
	if v == nil {
		return cty.NullVal(cty.DynamicPseudoType)
	}
	switch val := v.(type) {
	case string:
		return cty.StringVal(val)
	case bool:
		return cty.BoolVal(val)
	case float64:
		return cty.NumberFloatVal(val)
	case []interface{}:
		if len(val) == 0 {
			return cty.EmptyTupleVal
		}
		elems := make([]cty.Value, len(val))
		for i, e := range val {
			elems[i] = interfaceToCty(e)
		}
		return cty.TupleVal(elems)
	case map[string]interface{}:
		if len(val) == 0 {
			return cty.EmptyObjectVal
		}
		attrs := map[string]cty.Value{}
		for k, e := range val {
			attrs[k] = interfaceToCty(e)
		}
		return cty.ObjectVal(attrs)
	default:
		// Fallback: marshal to JSON and unmarshal as cty
		data, err := ctyjson.Marshal(cty.StringVal(fmt.Sprintf("%v", v)), cty.String)
		if err != nil {
			return cty.StringVal(fmt.Sprintf("%v", v))
		}
		val2, err := ctyjson.Unmarshal(data, cty.String)
		if err != nil {
			return cty.StringVal(fmt.Sprintf("%v", v))
		}
		return val2
	}
}

// findComponentNode finds the component node by resource name in the tree.
func findComponentNode(tree []*componentNode, resourceName string) *componentNode {
	for _, node := range tree {
		if node.resourceName == resourceName {
			return node
		}
		if found := findComponentNode(node.children, resourceName); found != nil {
			return found
		}
	}
	return nil
}

// buildMetaArgContext builds cty values for count.index and each.key/each.value
// based on the component's key from the TF state address.
func buildMetaArgContext(key string) map[string]cty.Value {
	vars := map[string]cty.Value{}

	// Try parsing as integer (count index)
	var idx int
	if _, err := fmt.Sscanf(key, "%d", &idx); err == nil {
		// count-based: count = { index = N }
		vars["count"] = cty.ObjectVal(map[string]cty.Value{
			"index": cty.NumberIntVal(int64(idx)),
		})
	} else {
		// for_each-based: each = { key = "K", value = "K" }
		vars["each"] = cty.ObjectVal(map[string]cty.Value{
			"key":   cty.StringVal(key),
			"value": cty.StringVal(key),
		})
	}

	return vars
}
