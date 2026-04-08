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
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/bridge"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/providermap"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tofu"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
)

// ComponentMap is the top-level structure written to the component-map.json sidecar file.
// It describes every TF module instance as a component, including its resources and interface.
type ComponentMap struct {
	Modules map[string]*ModuleMapEntry `json:"modules"`
}

// ModuleMapEntry describes a single TF module instance mapped to a Pulumi component.
type ModuleMapEntry struct {
	TerraformPath string                     `json:"terraformPath"`
	Source        string                     `json:"source,omitempty"`
	IndexKey      string                     `json:"indexKey,omitempty"`
	IndexType     string                     `json:"indexType,omitempty"`
	Resources     []string                   `json:"resources"`
	Interface     *ModuleInterface           `json:"interface,omitempty"`
	Modules       map[string]*ModuleMapEntry `json:"modules"`
}

// ModuleInterface describes the inputs and outputs of a component.
type ModuleInterface struct {
	Inputs  []ModuleInterfaceField `json:"inputs"`
	Outputs []ModuleInterfaceField `json:"outputs"`
}

// ModuleInterfaceField describes a single input or output field of a component interface.
type ModuleInterfaceField struct {
	Name           string      `json:"name"`
	Type           interface{} `json:"type,omitempty"`
	Required       bool        `json:"required,omitempty"`
	Default        interface{} `json:"default,omitempty"`
	Description    string      `json:"description,omitempty"`
	Expression     string      `json:"expression,omitempty"`
	EvaluatedValue interface{} `json:"evaluatedValue,omitempty"`
}

// ComponentMapData is an intermediate struct used to pass data from convertState
// to the component map writer.
type ComponentMapData struct {
	Tree       []*componentNode
	Components []PulumiResource
	Metadata   *ComponentSchemaMetadata
}

// stateResourceInfo holds the fields from tfjson.StateResource needed for resource matching.
type stateResourceInfo struct {
	Address      string
	Type         string
	Name         string
	ProviderName string
}

// buildComponentMap constructs a ComponentMap from the component tree, TF state, metadata,
// and provider information.
func buildComponentMap(
	tree []*componentNode,
	tfState *tfjson.State,
	metadata *ComponentSchemaMetadata,
	components []PulumiResource,
	pulumiProviders map[providermap.TerraformProviderName]*ProviderWithMetadata,
	stackName, projectName string,
) *ComponentMap {
	var allResources []stateResourceInfo
	tofu.VisitResources(tfState, func(r *tfjson.StateResource) error {
		allResources = append(allResources, stateResourceInfo{
			Address:      r.Address,
			Type:         r.Type,
			Name:         r.Name,
			ProviderName: r.ProviderName,
		})
		return nil
	}, &tofu.VisitOptions{})

	cm := &ComponentMap{
		Modules: map[string]*ModuleMapEntry{},
	}

	for _, node := range tree {
		key := moduleMapKey(node)
		cm.Modules[key] = buildModuleMapEntry(node, allResources, metadata, components, pulumiProviders, stackName, projectName)
	}

	return cm
}

// buildModuleMapEntry recursively builds a ModuleMapEntry for a single componentNode.
func buildModuleMapEntry(
	node *componentNode,
	allResources []stateResourceInfo,
	metadata *ComponentSchemaMetadata,
	components []PulumiResource,
	pulumiProviders map[providermap.TerraformProviderName]*ProviderWithMetadata,
	stackName, projectName string,
) *ModuleMapEntry {
	entry := &ModuleMapEntry{
		TerraformPath: node.modulePath,
		Modules:       map[string]*ModuleMapEntry{},
	}

	// IndexKey and IndexType
	if node.key != "" {
		entry.IndexKey = node.key
		if _, err := strconv.Atoi(node.key); err == nil {
			entry.IndexType = "count"
		} else {
			entry.IndexType = "for_each"
		}
	}

	// Source from metadata
	if metadata != nil {
		moduleKey := "module." + node.name
		if schema, ok := metadata.Components[moduleKey]; ok {
			entry.Source = schema.Source
		}
	}

	// Match resources from state to this module, collecting URNs
	for _, res := range allResources {
		segments := parseModuleSegments(res.Address)
		resModulePath := buildModulePath(segments)
		if resModulePath != node.modulePath {
			continue
		}

		pulumiType := ""
		prov, provOk := pulumiProviders[providermap.TerraformProviderName(res.ProviderName)]
		if provOk {
			tok, err := bridge.PulumiTypeToken(res.Type, prov.Provider)
			if err == nil {
				pulumiType = string(tok)
			}
		}

		flatName := PulumiNameFromTerraformAddress(res.Address, res.Type)
		urn := fmt.Sprintf("urn:pulumi:%s::%s::%s::%s", stackName, projectName, pulumiType, flatName)
		entry.Resources = append(entry.Resources, urn)
	}

	// Interface from metadata + component evaluated values
	if metadata != nil {
		moduleKey := "module." + node.name
		if schema, ok := metadata.Components[moduleKey]; ok {
			iface := &ModuleInterface{}

			// Find the matching component for evaluated values
			var matchingComp *PulumiResource
			for i := range components {
				if components[i].Name == node.resourceName {
					matchingComp = &components[i]
					break
				}
			}

			// Build inputs
			for _, field := range schema.Inputs {
				mif := ModuleInterfaceField{
					Name:        field.Name,
					Type:        field.Type,
					Required:    field.Required,
					Default:     field.Default,
					Description: field.Description,
				}
				if matchingComp != nil {
					if val, ok := matchingComp.Inputs[resource.PropertyKey(field.Name)]; ok {
						mif.EvaluatedValue = propertyValueToInterface(val)
					}
				}
				iface.Inputs = append(iface.Inputs, mif)
			}

			// Build outputs
			for _, field := range schema.Outputs {
				mif := ModuleInterfaceField{
					Name:        field.Name,
					Type:        field.Type,
					Description: field.Description,
				}
				if matchingComp != nil {
					if val, ok := matchingComp.Outputs[resource.PropertyKey(field.Name)]; ok {
						mif.EvaluatedValue = propertyValueToInterface(val)
					}
				}
				iface.Outputs = append(iface.Outputs, mif)
			}

			if len(iface.Inputs) > 0 || len(iface.Outputs) > 0 {
				entry.Interface = iface
			}
		}
	}

	// Recurse into children
	for _, child := range node.children {
		key := moduleMapKey(child)
		entry.Modules[key] = buildModuleMapEntry(child, allResources, metadata, components, pulumiProviders, stackName, projectName)
	}

	return entry
}

// moduleMapKey builds the map key for a componentNode.
// Non-indexed: "name", indexed: "name[0]" or "name[\"key\"]".
func moduleMapKey(node *componentNode) string {
	if node.key == "" {
		return node.name
	}
	return node.name + "[" + formatKey(node.key) + "]"
}

// propertyValueToInterface converts a Pulumi PropertyValue to a plain Go interface{}
// suitable for JSON serialization.
func propertyValueToInterface(v resource.PropertyValue) interface{} {
	if v.IsNull() {
		return nil
	}
	if v.IsSecret() {
		// Unwrap secrets to get the underlying value
		return propertyValueToInterface(v.SecretValue().Element)
	}
	if v.IsComputed() {
		return nil
	}
	if v.IsOutput() {
		ov := v.OutputValue()
		if ov.Known {
			return propertyValueToInterface(ov.Element)
		}
		return nil
	}
	if v.IsBool() {
		return v.BoolValue()
	}
	if v.IsNumber() {
		return v.NumberValue()
	}
	if v.IsString() {
		return v.StringValue()
	}
	if v.IsArray() {
		arr := v.ArrayValue()
		result := make([]interface{}, len(arr))
		for i, elem := range arr {
			result[i] = propertyValueToInterface(elem)
		}
		return result
	}
	if v.IsObject() {
		obj := v.ObjectValue()
		result := make(map[string]interface{}, len(obj))
		for k, elem := range obj {
			result[string(k)] = propertyValueToInterface(elem)
		}
		return result
	}
	return nil
}

// WriteComponentMap serializes a ComponentMap to a JSON file.
func WriteComponentMap(cm *ComponentMap, path string) error {
	bytes, err := json.MarshalIndent(cm, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling component map: %w", err)
	}
	bytes = append(bytes, '\n')
	if err := os.WriteFile(path, bytes, 0o600); err != nil {
		return fmt.Errorf("writing component map: %w", err)
	}
	return nil
}

// stripModulePrefix removes the module path prefix from a resource address,
// returning just the "resourceType.resourceName" portion.
func stripModulePrefix(address string) string {
	parts := splitAddressParts(address)
	// Skip all "module.xxx" segments
	i := 0
	for i < len(parts) {
		if parts[i] == "module" && i+1 < len(parts) {
			i += 2
		} else {
			break
		}
	}
	return strings.Join(parts[i:], ".")
}
