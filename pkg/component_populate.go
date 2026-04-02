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

	hclpkg "github.com/pulumi/pulumi-tool-terraform-migrate/pkg/hcl"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/zclconf/go-cty/cty"
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
) (*ComponentSchemaMetadata, error) {
	if tfSourceDir == "" {
		return nil, nil
	}

	// Parse module call sites from the root TF source directory
	callSites, err := hclpkg.ParseModuleCallSites(tfSourceDir)
	if err != nil {
		// Not fatal — HCL source may not be available
		fmt.Fprintf(os.Stderr, "Warning: failed to parse module call sites from %s: %v\n", tfSourceDir, err)
		return nil, nil
	}

	// Load tfvars for variable resolution
	tfvarsPath := filepath.Join(tfSourceDir, "terraform.tfvars")
	tfvars, err := hclpkg.LoadTfvars(tfvarsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load tfvars: %v\n", err)
		tfvars = map[string]cty.Value{}
	}

	// Build a lookup from module name to call site
	callSiteMap := map[string]*hclpkg.ModuleCallSite{}
	for i := range callSites {
		callSiteMap[callSites[i].Name] = &callSites[i]
	}

	// Collect parsed variables/outputs for metadata (when populateInputs=false)
	parsedVariables := map[string][]hclpkg.ModuleVariable{}
	parsedOutputs := map[string][]hclpkg.ModuleOutput{}
	resolvedSources := map[string]string{}

	// Process each component
	for i, comp := range components {
		// Find the component node to get module name and key
		node := findComponentNode(componentTree, comp.Name)
		if node == nil {
			continue
		}
		moduleName := node.name

		// Resolve HCL source path
		sourcePath := ""
		if override, ok := sourceOverrides["module."+moduleName]; ok {
			sourcePath = override
		} else if callSite, ok := callSiteMap[moduleName]; ok {
			// Auto-resolve from call site source attribute (local paths only)
			if hclpkg.IsLocalModuleSource(callSite.Source) {
				sourcePath = filepath.Join(tfSourceDir, callSite.Source)
			}
		}
		if sourcePath != "" {
			resolvedSources["module."+moduleName] = sourcePath
		}

		// Parse module variables (needed for both metadata and default merging)
		if sourcePath != "" {
			if vars, err := hclpkg.ParseModuleVariables(sourcePath); err == nil {
				parsedVariables[moduleName] = vars
			}
		}

		// Populate inputs from call site argument evaluation (only when populateInputs=true)
		callSite, hasCallSite := callSiteMap[moduleName]
		if populateInputs && hasCallSite && len(callSite.Arguments) > 0 {
			// Build eval context variables including count.index / each.key
			evalVars := map[string]cty.Value{}
			maps.Copy(evalVars, tfvars)

			// Build meta-argument context (count.index, each.key/each.value)
			var metaVars map[string]cty.Value
			if node.key != "" {
				metaVars = buildMetaArgContext(node.key)
			}

			evalCtx := hclpkg.NewEvalContext(evalVars, nil, nil)
			if metaVars != nil {
				evalCtx.AddVariables(metaVars)
			}

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

		// Populate outputs from parsed module output declarations
		if sourcePath != "" {
			outputs, err := hclpkg.ParseModuleOutputs(sourcePath)
			if err == nil {
				parsedOutputs[moduleName] = outputs
				outputMap := resource.PropertyMap{}
				for _, o := range outputs {
					// Output values come from TF state (Phase 2 raw state reading),
					// not from HCL expression evaluation. For now, record output names
					// with empty values as placeholders.
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

	// Build and return metadata when populateInputs=false
	if !populateInputs {
		metadata := buildComponentSchemaMetadata(components, componentTree, parsedVariables, parsedOutputs, resolvedSources)
		return metadata, nil
	}

	return nil, nil
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
