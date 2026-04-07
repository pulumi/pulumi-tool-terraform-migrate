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

	"github.com/pulumi/pulumi/pkg/v3/codegen/schema"
)

// ComponentField represents a single input or output field of a component.
type ComponentField struct {
	Name     string
	Required bool
}

// ComponentInterface represents the inputs and outputs of a component resource.
type ComponentInterface struct {
	Inputs  []ComponentField
	Outputs []ComponentField
}

// LoadComponentSchema loads a Pulumi package schema JSON file and extracts the
// component interface (inputs and outputs) for the given component type token.
func LoadComponentSchema(schemaPath string, componentType string) (*ComponentInterface, error) {
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("reading schema file: %w", err)
	}

	var spec schema.PackageSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parsing schema JSON: %w", err)
	}

	pkg, diags, err := schema.BindSpec(spec, nil, schema.ValidationOptions{})
	if err != nil {
		return nil, fmt.Errorf("binding schema spec: %w", err)
	}
	if diags.HasErrors() {
		return nil, fmt.Errorf("schema validation errors: %s", diags.Error())
	}

	resource, ok := pkg.GetResource(componentType)
	if !ok {
		return nil, fmt.Errorf("component type %q not found in schema", componentType)
	}
	if !resource.IsComponent {
		return nil, fmt.Errorf("resource %q is not a component (isComponent=false)", componentType)
	}

	iface := &ComponentInterface{}

	for _, prop := range resource.InputProperties {
		iface.Inputs = append(iface.Inputs, ComponentField{
			Name:     prop.Name,
			Required: prop.IsRequired(),
		})
	}

	for _, prop := range resource.Properties {
		iface.Outputs = append(iface.Outputs, ComponentField{
			Name: prop.Name,
		})
	}

	return iface, nil
}

// ValidateAgainstSchema validates that a parsed component interface matches a schema.
// Schema is source of truth — mismatch is an error. Type compatibility is handled by
// the value conversion pipeline (cty.Value → resource.PropertyMap via tfbridge), not here.
func ValidateAgainstSchema(parsed *ComponentInterface, schemaIface *ComponentInterface) error {
	parsedInputs := map[string]bool{}
	for _, f := range parsed.Inputs {
		parsedInputs[f.Name] = true
	}
	for _, f := range schemaIface.Inputs {
		if f.Required && !parsedInputs[f.Name] {
			return fmt.Errorf("input %q is required by schema but not found in parsed interface", f.Name)
		}
	}

	schemaOutputs := map[string]bool{}
	for _, f := range schemaIface.Outputs {
		schemaOutputs[f.Name] = true
	}
	for _, f := range parsed.Outputs {
		if !schemaOutputs[f.Name] {
			return fmt.Errorf("output %q not in schema", f.Name)
		}
	}

	return nil
}
