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
	"strings"

	hclpkg "github.com/pulumi/pulumi-tool-terraform-migrate/pkg/hcl"
	"github.com/zclconf/go-cty/cty"
)

// ComponentSchemaMetadata holds the parsed component interface for each module,
// written as a sidecar file when --component-inputs=false.
type ComponentSchemaMetadata struct {
	Components map[string]ComponentSchema `json:"components"`
}

// ComponentSchema describes a single component's interface (inputs and outputs).
type ComponentSchema struct {
	Type    string               `json:"type"`
	Source  string               `json:"source,omitempty"`
	Inputs  []ComponentFieldMeta `json:"inputs"`
	Outputs []ComponentFieldMeta `json:"outputs"`
}

// ComponentFieldMeta describes a single input or output field.
// Type uses Pulumi package schema format: "string", "number", "boolean", "integer"
// for primitives, or {"type": "array", "items": {...}} for collections.
type ComponentFieldMeta struct {
	Name        string      `json:"name"`
	Type        interface{} `json:"type,omitempty"`
	Required    bool        `json:"required,omitempty"`
	Default     interface{} `json:"default,omitempty"`
	Description string      `json:"description,omitempty"`
}

// buildComponentSchemaMetadata constructs metadata from parsed HCL module definitions.
func buildComponentSchemaMetadata(
	components []PulumiResource,
	componentTree []*componentNode,
	variables map[string][]hclpkg.ModuleVariable,
	outputs map[string][]hclpkg.ModuleOutput,
	sources map[string]string,
) *ComponentSchemaMetadata {
	metadata := &ComponentSchemaMetadata{
		Components: map[string]ComponentSchema{},
	}

	for _, comp := range components {
		node := findComponentNode(componentTree, comp.Name)
		if node == nil {
			continue
		}
		moduleKey := "module." + node.name

		schema := ComponentSchema{
			Type:   comp.Type,
			Source: sources[moduleKey],
		}

		if vars, ok := variables[node.name]; ok {
			for _, v := range vars {
				field := ComponentFieldMeta{
					Name:        v.Name,
					Type:        hclTypeToPulumiSchemaType(v.Type),
					Required:    v.Default == nil,
					Description: v.Description,
				}
				if v.Default != nil {
					field.Default = ctyValueToInterface(*v.Default)
				}
				schema.Inputs = append(schema.Inputs, field)
			}
		}

		if outs, ok := outputs[node.name]; ok {
			for _, o := range outs {
				schema.Outputs = append(schema.Outputs, ComponentFieldMeta{
					Name:        o.Name,
					Description: o.Description,
				})
			}
		}

		metadata.Components[moduleKey] = schema
	}

	return metadata
}

// hclTypeToPulumiSchemaType converts an HCL type constraint string to
// Pulumi package schema type format.
//
// Primitives: "string" → "string", "number" → "number", "bool" → "boolean"
// Collections: "list(string)" → {"type": "array", "items": {"type": "string"}}
// Maps: "map(string)" → {"type": "object", "additionalProperties": {"type": "string"}}
// Sets: "set(string)" → {"type": "array", "items": {"type": "string"}}
// Unknown/empty: returns the original string as-is.
func hclTypeToPulumiSchemaType(hclType string) interface{} {
	if hclType == "" {
		return nil
	}

	switch hclType {
	case "string":
		return "string"
	case "number":
		return "number"
	case "bool":
		return "boolean"
	case "any":
		return "object"
	}

	// list(T), set(T) → {"type": "array", "items": <T>}
	if strings.HasPrefix(hclType, "list(") || strings.HasPrefix(hclType, "set(") {
		inner := hclType[strings.Index(hclType, "(")+1 : len(hclType)-1]
		return map[string]interface{}{
			"type":  "array",
			"items": hclTypeToPulumiSchemaType(inner),
		}
	}

	// map(T) → {"type": "object", "additionalProperties": <T>}
	if strings.HasPrefix(hclType, "map(") {
		inner := hclType[4 : len(hclType)-1]
		return map[string]interface{}{
			"type":                 "object",
			"additionalProperties": hclTypeToPulumiSchemaType(inner),
		}
	}

	// tuple([...]) → {"type": "array"} (no item type info)
	if strings.HasPrefix(hclType, "tuple(") {
		return map[string]interface{}{"type": "array"}
	}

	// object({...}) → {"type": "object"} (TODO: parse property types)
	if strings.HasPrefix(hclType, "object(") {
		return map[string]interface{}{"type": "object"}
	}

	// Unknown type — return as-is
	return hclType
}

// ctyValueToInterface converts a cty.Value to a plain Go interface{} for JSON serialization.
func ctyValueToInterface(v cty.Value) interface{} {
	if v.IsNull() || !v.IsKnown() {
		return nil
	}
	ty := v.Type()
	switch {
	case ty == cty.String:
		return v.AsString()
	case ty == cty.Bool:
		return v.True()
	case ty == cty.Number:
		bf := v.AsBigFloat()
		if bf.IsInt() {
			i, _ := bf.Int64()
			return i
		}
		f, _ := bf.Float64()
		return f
	case ty.IsListType() || ty.IsTupleType() || ty.IsSetType():
		var result []interface{}
		for it := v.ElementIterator(); it.Next(); {
			_, elem := it.Element()
			result = append(result, ctyValueToInterface(elem))
		}
		return result
	case ty.IsMapType() || ty.IsObjectType():
		result := map[string]interface{}{}
		for it := v.ElementIterator(); it.Next(); {
			key, elem := it.Element()
			result[key.AsString()] = ctyValueToInterface(elem)
		}
		return result
	default:
		return nil
	}
}

// WriteComponentSchemaMetadata writes the metadata to a JSON file.
func WriteComponentSchemaMetadata(metadata *ComponentSchemaMetadata, path string) error {
	bytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling component schema metadata: %w", err)
	}
	if err := os.WriteFile(path, bytes, 0o600); err != nil {
		return fmt.Errorf("writing component schema metadata: %w", err)
	}
	return nil
}
