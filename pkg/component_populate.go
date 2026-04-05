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

	// Merge root variable defaults into tfvars (tfvars take precedence).
	// This ensures locals that reference var.* with defaults can evaluate.
	rootVars, _ := hclpkg.ParseModuleVariables(tfSourceDir)
	for _, v := range rootVars {
		if _, alreadySet := tfvars[v.Name]; !alreadySet && v.Default != nil {
			tfvars[v.Name] = *v.Default
		}
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
	// Two phases: (1) resolve sources and evaluate leaf modules, (2) evaluate parent modules
	// with child outputs available.
	moduleOutputValues := map[string]map[string]cty.Value{}
	resolvedSources := map[string]string{}

	// Phase 1: resolve all source paths and evaluate leaf module outputs
	var parentNodes []*componentNode
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

		// Defer parent modules (those with children) to phase 2
		if len(node.children) > 0 {
			parentNodes = append(parentNodes, node)
			continue
		}

		evaluatePrePassOutputs(node, sourcePath, scopedAttrs, nil, moduleOutputValues)
	}

	// Resolve child module source paths from parent directories so
	// buildChildModuleOutputs can find them in phase 2.
	for _, parentNode := range parentNodes {
		parentSource := resolvedSources["module."+parentNode.name]
		if parentSource == "" {
			continue
		}
		parentCallSites, _ := hclpkg.ParseModuleCallSites(parentSource)
		for _, cs := range parentCallSites {
			childKey := "module." + cs.Name
			if _, already := resolvedSources[childKey]; already {
				continue
			}
			if hclpkg.IsLocalModuleSource(cs.Source) {
				resolvedSources[childKey] = filepath.Join(parentSource, cs.Source)
			}
		}
		// Also evaluate any child nodes that weren't resolved from root call sites
		for _, child := range parentNode.children {
			if _, already := moduleOutputValues[child.name]; already {
				continue
			}
			childSource := resolvedSources["module."+child.name]
			if childSource != "" {
				evaluatePrePassOutputs(child, childSource, scopedAttrs, nil, moduleOutputValues)
			}
		}
	}

	// Phase 2: evaluate parent module outputs with child outputs available
	for _, node := range parentNodes {
		sourcePath := resolvedSources["module."+node.name]
		childOutputs := buildChildModuleOutputs(node, moduleOutputValues, resolvedSources, cachedModuleSources)
		evaluatePrePassOutputs(node, sourcePath, scopedAttrs, childOutputs, moduleOutputValues)
	}

	// Cache parsed call sites per directory (root already parsed)
	callSiteCache := map[string]map[string]*hclpkg.ModuleCallSite{
		tfSourceDir: callSiteMap,
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

		// For nested modules, try parsing call sites from the parent module's source dir
		isNestedCallSite := false
		var parentNode *componentNode
		if !hasCallSite {
			parentNode = findParentComponentNode(componentTree, node.resourceName)
			if parentNode != nil {
				parentSource := resolvedSources["module."+parentNode.name]
				if parentSource != "" {
					parentCallSites := parseCallSitesCached(parentSource, callSiteCache)
					if cs, ok := parentCallSites[moduleName]; ok {
						callSite = cs
						hasCallSite = true
						isNestedCallSite = true
					}
				}
			}
		}

		if populateInputs && hasCallSite && len(callSite.Arguments) > 0 {
			evalVars := map[string]cty.Value{}
			if isNestedCallSite && parentNode != nil {
				// Nested call site: use the parent component's inputs as var.* scope
				parentComp := findComponentByName(components, parentNode.resourceName)
				if parentComp != nil && parentComp.Inputs != nil {
					maps.Copy(evalVars, hclpkg.PulumiPropertyMapToCtyMap(parentComp.Inputs))
				}
				// Also merge parent module's variable defaults for any vars not set
				parentSource := resolvedSources["module."+parentNode.name]
				if parentSource != "" {
					parentVars, _ := hclpkg.ParseModuleVariables(parentSource)
					for _, v := range parentVars {
						if _, alreadySet := evalVars[v.Name]; !alreadySet && v.Default != nil {
							evalVars[v.Name] = *v.Default
						}
					}
				}
				// Convert null-typed defaults to zero values so functions like
				// coalesce() don't reject mixed null/concrete types.
				// Also convert cty.NilVal (Go zero-value from PropertyMap round-trip)
				// to empty string, since NilVal panics on .IsNull()/.Type().
				for k, v := range evalVars {
					if v == cty.NilVal {
						evalVars[k] = cty.StringVal("")
					} else if v.IsNull() {
						evalVars[k] = ctyNullToZero(v)
					}
				}
			} else {
				maps.Copy(evalVars, tfvars)
			}

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
			// For nested call sites, use parent module's locals instead of root locals
			localsSourceDir := tfSourceDir
			if isNestedCallSite && parentNode != nil {
				parentSource := resolvedSources["module."+parentNode.name]
				if parentSource != "" {
					localsSourceDir = parentSource
				}
			}
			evaluateAndAddLocals(localsSourceDir, evalCtx)

			inputs := resource.PropertyMap{}
			for argName, argExpr := range callSite.Arguments {
				val, err := evalCtx.EvaluateExpression(argExpr)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to evaluate argument %q for module.%s: %v\n", argName, moduleName, err)
					continue
				}
				inputs[resource.PropertyKey(argName)] = hclpkg.CtyValueToPulumiPropertyValue(val)
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
				// Include variable defaults in the eval context (but not in state inputs)
				// so that output expressions referencing unset variables can still resolve.
				if vars, ok := parsedVariables[moduleName]; ok {
					for _, v := range vars {
						if _, alreadySet := moduleVars[v.Name]; !alreadySet && v.Default != nil {
							moduleVars[v.Name] = *v.Default
						}
					}
				}

				// Build child module output cross-refs for parent modules
				// (e.g., rdsdb needs module.db_instance.* outputs to evaluate its own outputs)
				childOutputs := buildChildModuleOutputs(node, moduleOutputValues, resolvedSources, cachedModuleSources)

				outputEvalCtx := hclpkg.NewEvalContext(moduleVars, moduleResourceAttrs, childOutputs)

				// Register missing resource types BEFORE locals evaluation so
				// locals referencing conditional resources (count=0) resolve.
				registerMissingResourceTypes(sourcePath, moduleResourceAttrs, outputEvalCtx, moduleName)

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
	// Try exact modulePath first (e.g., "module.vpc"), then base name without
	// index/key for for_each/count instances (e.g., "module.ec2_private_app1"
	// instead of "module.ec2_private_app1[\"0\"]").
	if cached, ok := cachedModuleSources[node.modulePath]; ok {
		return cached
	}
	if cached, ok := cachedModuleSources["module."+node.name]; ok {
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

// ctyNullToZero converts a cty.NullVal to the zero value for its type.
// This prevents type mismatch errors in functions like coalesce() that
// reject mixed null/concrete types.
func ctyNullToZero(v cty.Value) cty.Value {
	ty := v.Type()
	switch {
	case ty == cty.String:
		return cty.StringVal("")
	case ty == cty.Number:
		return cty.NumberIntVal(0)
	case ty == cty.Bool:
		return cty.BoolVal(false)
	case ty == cty.DynamicPseudoType:
		// HCL parses "default = null" without type context as DynamicPseudoType.
		// Default to empty string since most Terraform variables are strings.
		return cty.StringVal("")
	default:
		return v // leave complex types as-is
	}
}

func evaluatePrePassOutputs(
	node *componentNode,
	sourcePath string,
	scopedAttrs scopedResourceAttrs,
	childOutputs map[string]map[string]cty.Value,
	moduleOutputValues map[string]map[string]cty.Value,
) {
	outputs, err := hclpkg.ParseModuleOutputs(sourcePath)
	if err != nil || len(outputs) == 0 {
		return
	}
	moduleResourceAttrs := scopedAttrs.forModule(node.modulePath)
	outputEvalCtx := hclpkg.NewEvalContext(nil, moduleResourceAttrs, childOutputs)

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

	// Post-process: for indexed resources (this[0], this[1], this["key"]),
	// create a base name entry (this) as a tuple/object so HCL expressions
	// like resource.this[0].attr resolve correctly.
	for _, typeMap := range result {
		for _, instances := range typeMap {
			grouped := map[string][]cty.Value{} // baseName → [instance0, instance1, ...]
			for name, val := range instances {
				if idx := strings.Index(name, "["); idx > 0 {
					baseName := name[:idx]
					grouped[baseName] = append(grouped[baseName], val)
				}
			}
			for baseName, vals := range grouped {
				if _, exists := instances[baseName]; !exists {
					if len(vals) == 1 {
						instances[baseName] = vals[0]
					} else {
						instances[baseName] = cty.TupleVal(vals)
					}
				}
			}
		}
	}

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

// findParentComponentNode finds the parent of a node by searching for which node has it as a child.
func findParentComponentNode(tree []*componentNode, childResourceName string) *componentNode {
	for _, node := range tree {
		for _, child := range node.children {
			if child.resourceName == childResourceName {
				return node
			}
		}
		if found := findParentComponentNode(node.children, childResourceName); found != nil {
			return found
		}
	}
	return nil
}

// parseCallSitesCached parses module call sites from a directory, using a cache to avoid re-parsing.
func parseCallSitesCached(dir string, cache map[string]map[string]*hclpkg.ModuleCallSite) map[string]*hclpkg.ModuleCallSite {
	if cached, ok := cache[dir]; ok {
		return cached
	}
	calls, err := hclpkg.ParseModuleCallSites(dir)
	if err != nil {
		cache[dir] = map[string]*hclpkg.ModuleCallSite{}
		return cache[dir]
	}
	result := map[string]*hclpkg.ModuleCallSite{}
	for i := range calls {
		result[calls[i].Name] = &calls[i]
	}
	cache[dir] = result
	return result
}

// buildChildModuleOutputs collects resolved outputs from child components of a given node.
// For a parent module like "rdsdb" with children "db_instance", "db_option_group", etc.,
// this returns {"db_instance": {output1: val1, ...}, "db_option_group": {...}}.
//
// For children not in moduleOutputValues (e.g., zero-instance or no managed resources),
// the function parses output declarations from the child's resolved source and registers
// them as empty strings so parent output expressions like module.child.name resolve.
//
// cachedModuleSources provides fallback resolution for children not in resolvedSources
// (e.g., data-source-only modules that don't appear in the component tree).
func buildChildModuleOutputs(
	node *componentNode,
	moduleOutputValues map[string]map[string]cty.Value,
	resolvedSources map[string]string,
	cachedModuleSources map[string]string,
) map[string]map[string]cty.Value {
	result := map[string]map[string]cty.Value{}

	// Include outputs from children in the component tree
	for _, child := range node.children {
		if outputs, ok := moduleOutputValues[child.name]; ok {
			result[child.name] = outputs
			continue
		}
		sourcePath := resolvedSources["module."+child.name]
		if sourcePath == "" {
			continue
		}
		outputs, err := hclpkg.ParseModuleOutputs(sourcePath)
		if err != nil || len(outputs) == 0 {
			continue
		}
		emptyOutputs := map[string]cty.Value{}
		for _, o := range outputs {
			emptyOutputs[o.Name] = cty.StringVal("")
		}
		result[child.name] = emptyOutputs
	}

	// Also discover child modules from the module cache that don't appear in the
	// component tree (e.g., data-source-only modules with no managed resources).
	// The cache key pattern is "module.parent.module.child" for nested modules.
	prefix := node.modulePath + ".module."
	for cacheKey, dir := range cachedModuleSources {
		if !strings.HasPrefix(cacheKey, prefix) {
			continue
		}
		// Extract child module name from "module.parent.module.child"
		childName := strings.TrimPrefix(cacheKey, prefix)
		if strings.Contains(childName, ".") {
			continue // skip deeper nesting (grandchildren)
		}
		if _, already := result[childName]; already {
			continue
		}
		outputs, err := hclpkg.ParseModuleOutputs(dir)
		if err != nil || len(outputs) == 0 {
			continue
		}
		emptyOutputs := map[string]cty.Value{}
		for _, o := range outputs {
			emptyOutputs[o.Name] = cty.StringVal("")
		}
		result[childName] = emptyOutputs
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// registerMissingResourceTypes scans the module HCL source for resource blocks and
// registers any not in the scoped attr map with empty default values.
// This handles conditional resources (count=0) that don't exist in state.
// Registers as: type = {name = []} so expressions like aws_ebs_volume.this[0] resolve.
func registerMissingResourceTypes(
	sourcePath string,
	moduleResourceAttrs map[string]map[string]cty.Value,
	evalCtx *hclpkg.EvalContext,
	moduleName string,
) {
	resourceBlocks, err := hclpkg.ParseResourceBlocks(sourcePath)
	if err != nil {
		return
	}

	// Group by type, collect instance names and track count/for_each
	type missingResource struct {
		hasCount   bool
		hasForEach bool
	}
	typeInstances := map[string]map[string]missingResource{} // type → name → info
	for _, rb := range resourceBlocks {
		if moduleResourceAttrs != nil {
			if instances, ok := moduleResourceAttrs[rb.Type]; ok {
				if _, hasName := instances[rb.Name]; hasName {
					continue // this specific instance exists in state
				}
			}
		}
		if _, ok := typeInstances[rb.Type]; !ok {
			typeInstances[rb.Type] = map[string]missingResource{}
		}
		typeInstances[rb.Type][rb.Name] = missingResource{
			hasCount:   rb.HasCount,
			hasForEach: rb.HasForEach,
		}
	}

	missingVars := map[string]cty.Value{}
	for resType, names := range typeInstances {
		instances := map[string]cty.Value{}

		// Keep existing instances from state
		if moduleResourceAttrs != nil {
			if existing, ok := moduleResourceAttrs[resType]; ok {
				for k, v := range existing {
					instances[k] = v
				}
			}
		}

		var sampleName string
		for name := range names {
			sampleName = name
			break
		}
		template := buildNullAttributeTemplate(instances, sourcePath, resType, sampleName)
		for name, info := range names {
			if _, exists := instances[name]; exists {
				continue
			}
			fmt.Fprintf(os.Stderr, "Note: %s.%s not in state for module.%s (likely count=0), defaulting to null\n", resType, name, moduleName)
			// Resources with count or for_each that have zero instances in state
			// should be registered as empty tuples so splat/for-each resolve to [].
			// Resources without count/for_each use the template for .attr access.
			if info.hasCount || info.hasForEach {
				instances[name] = cty.EmptyTupleVal
			} else {
				instances[name] = template
			}
		}

		if len(instances) > 0 {
			missingVars[resType] = cty.ObjectVal(instances)
		}
	}

	if len(missingVars) > 0 {
		evalCtx.AddVariables(missingVars)
	}
}

// buildNullAttributeTemplate creates a cty object with the same attribute names as
// existing instances but all values set to null strings. Used for missing resource
// instances (count=0) so attribute access resolves to "" instead of panicking.
//
// When no instances exist, falls back to parsing the HCL source to discover attributes.
// If that also fails, uses a minimal template with common attrs (id, arn, tags, name).
func buildNullAttributeTemplate(instances map[string]cty.Value, sourcePath, resourceType, resourceName string) cty.Value {
	// Find any existing instance to use as a template
	for _, inst := range instances {
		if inst.Type().IsObjectType() {
			nullAttrs := map[string]cty.Value{}
			for name := range inst.Type().AttributeTypes() {
				nullAttrs[name] = cty.StringVal("")
			}
			if len(nullAttrs) > 0 {
				return cty.ObjectVal(nullAttrs)
			}
		}
		break
	}

	// No existing instances — try parsing the resource block from HCL
	if sourcePath != "" && resourceType != "" && resourceName != "" {
		attrNames, err := hclpkg.ParseResourceBlockAttrs(sourcePath, resourceType, resourceName)
		if err == nil && len(attrNames) > 0 {
			nullAttrs := map[string]cty.Value{}
			for _, name := range attrNames {
				nullAttrs[name] = cty.StringVal("")
			}
			return cty.ObjectVal(nullAttrs)
		}
	}

	return cty.EmptyObjectVal
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

// findComponentByName finds a PulumiResource by its Name field.
func findComponentByName(components []PulumiResource, name string) *PulumiResource {
	for i := range components {
		if components[i].Name == name {
			return &components[i]
		}
	}
	return nil
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

	// Set both count and each — we can't always distinguish count from for_each
	// (e.g., for_each = toset(["0", "1"]) uses numeric-looking string keys).
	// Setting both is safe since TF expressions only reference one or the other.
	var idx int
	if _, err := fmt.Sscanf(key, "%d", &idx); err == nil {
		vars["count"] = cty.ObjectVal(map[string]cty.Value{
			"index": cty.NumberIntVal(int64(idx)),
		})
	}
	vars["each"] = cty.ObjectVal(map[string]cty.Value{
		"key":   cty.StringVal(key),
		"value": cty.StringVal(key),
	})

	return vars
}
