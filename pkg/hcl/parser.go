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
	"maps"
	"path/filepath"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/typeexpr"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// ResourceBlock represents a resource type and name from a resource block declaration.
type ResourceBlock struct {
	Type        string // e.g., "aws_vpc"
	Name        string // e.g., "this"
	HasCount    bool   // resource has count meta-argument
	HasForEach  bool   // resource has for_each meta-argument
}

// ParseResourceBlocks extracts resource type+name pairs from .tf files in a directory.
func ParseResourceBlocks(dir string) ([]ResourceBlock, error) {
	files, err := parseTFFiles(dir)
	if err != nil {
		return nil, err
	}

	var blocks []ResourceBlock
	seen := map[string]bool{}
	for _, f := range files {
		body, ok := f.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}
		for _, block := range body.Blocks {
			if block.Type == "resource" && len(block.Labels) >= 2 {
				key := block.Labels[0] + "." + block.Labels[1]
				if !seen[key] {
					seen[key] = true
					rb := ResourceBlock{Type: block.Labels[0], Name: block.Labels[1]}
					if block.Body != nil {
						_, rb.HasCount = block.Body.Attributes["count"]
						_, rb.HasForEach = block.Body.Attributes["for_each"]
					}
					blocks = append(blocks, rb)
				}
			}
		}
	}
	return blocks, nil
}

// ModuleVariable represents a parsed variable block from a Terraform module.
type ModuleVariable struct {
	Name        string
	Type        string     // HCL type constraint string (e.g., "string", "map(string)")
	Default     *cty.Value // nil if no default (variable is required)
	Description string
}

// ModuleOutput represents a parsed output block from a Terraform module.
type ModuleOutput struct {
	Name        string
	Description string
	Expression  hcl.Expression // raw HCL expression for later evaluation
}

// ParseModuleVariables parses all .tf files in moduleDir and extracts variable blocks.
func ParseModuleVariables(moduleDir string) ([]ModuleVariable, error) {
	files, err := parseTFFiles(moduleDir)
	if err != nil {
		return nil, err
	}

	var vars []ModuleVariable
	for _, file := range files {
		body, ok := file.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}
		for _, block := range body.Blocks {
			if block.Type != "variable" || len(block.Labels) == 0 {
				continue
			}
			v := ModuleVariable{Name: block.Labels[0]}

			attrs, _ := block.Body.JustAttributes()
			if typeAttr, ok := attrs["type"]; ok {
				ty, diags := typeexpr.TypeConstraint(typeAttr.Expr)
				if !diags.HasErrors() {
					v.Type = typeexpr.TypeString(ty)
				}
			}
			if descAttr, ok := attrs["description"]; ok {
				val, diags := descAttr.Expr.Value(nil)
				if !diags.HasErrors() {
					v.Description = val.AsString()
				}
			}
			if defaultAttr, ok := attrs["default"]; ok {
				val, diags := defaultAttr.Expr.Value(nil)
				if !diags.HasErrors() {
					v.Default = &val
				}
			}

			vars = append(vars, v)
		}
	}
	return vars, nil
}

// ParseModuleOutputs parses all .tf files in moduleDir and extracts output blocks.
func ParseModuleOutputs(moduleDir string) ([]ModuleOutput, error) {
	files, err := parseTFFiles(moduleDir)
	if err != nil {
		return nil, err
	}

	var outputs []ModuleOutput
	for _, file := range files {
		body, ok := file.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}
		for _, block := range body.Blocks {
			if block.Type != "output" || len(block.Labels) == 0 {
				continue
			}
			o := ModuleOutput{Name: block.Labels[0]}

			attrs, _ := block.Body.JustAttributes()
			if descAttr, ok := attrs["description"]; ok {
				val, diags := descAttr.Expr.Value(nil)
				if !diags.HasErrors() {
					o.Description = val.AsString()
				}
			}
			if valueAttr, ok := attrs["value"]; ok {
				o.Expression = valueAttr.Expr
			}

			outputs = append(outputs, o)
		}
	}
	return outputs, nil
}

// ModuleCallSite represents a parsed module block from a root Terraform configuration.
type ModuleCallSite struct {
	Name      string
	Source    string
	Arguments map[string]hcl.Expression // argument name -> expression (excludes source, version, count, for_each, providers, depends_on)
}

// metaArguments are module block attributes that are not passed as input arguments.
var metaArguments = map[string]bool{
	"source":     true,
	"version":    true,
	"count":      true,
	"for_each":   true,
	"providers":  true,
	"depends_on": true,
}

// ParseModuleCallSites parses all .tf files in rootDir and extracts module call blocks.
func ParseModuleCallSites(rootDir string) ([]ModuleCallSite, error) {
	files, err := parseTFFiles(rootDir)
	if err != nil {
		return nil, err
	}

	var calls []ModuleCallSite
	for _, file := range files {
		body, ok := file.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}
		for _, block := range body.Blocks {
			if block.Type != "module" || len(block.Labels) == 0 {
				continue
			}
			c := ModuleCallSite{
				Name:      block.Labels[0],
				Arguments: map[string]hcl.Expression{},
			}

			attrs, _ := block.Body.JustAttributes()
			if sourceAttr, ok := attrs["source"]; ok {
				val, diags := sourceAttr.Expr.Value(nil)
				if !diags.HasErrors() {
					c.Source = val.AsString()
				}
			}
			for name, attr := range attrs {
				if !metaArguments[name] {
					c.Arguments[name] = attr.Expr
				}
			}

			calls = append(calls, c)
		}
	}
	return calls, nil
}

// LoadTfvars loads a terraform.tfvars file and returns the values as a map.
// Returns an empty map (not an error) if the file doesn't exist.
func LoadTfvars(path string) (map[string]cty.Value, error) {
	parser := hclparse.NewParser()
	f, diags := parser.ParseHCLFile(path)
	if diags.HasErrors() {
		// Check if file doesn't exist
		for _, d := range diags {
			if d.Summary == "Failed to read file" {
				return map[string]cty.Value{}, nil
			}
		}
		return nil, fmt.Errorf("parsing tfvars %s: %s", path, diags.Error())
	}

	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return map[string]cty.Value{}, nil
	}

	attrs, diags := body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("reading tfvars attributes: %s", diags.Error())
	}

	values := map[string]cty.Value{}
	for name, attr := range attrs {
		val, diags := attr.Expr.Value(nil)
		if !diags.HasErrors() {
			values[name] = val
		}
	}
	return values, nil
}

// LoadAllTfvars loads terraform.tfvars and all *.auto.tfvars files from a directory.
// terraform.tfvars is loaded first, then *.auto.tfvars in alphabetical order.
// Later values override earlier ones (matching Terraform's behavior).
func LoadAllTfvars(dir string) (map[string]cty.Value, error) {
	result := map[string]cty.Value{}

	tfvars, err := LoadTfvars(filepath.Join(dir, "terraform.tfvars"))
	if err != nil {
		return nil, err
	}
	maps.Copy(result, tfvars)

	autoFiles, _ := filepath.Glob(filepath.Join(dir, "*.auto.tfvars"))
	sort.Strings(autoFiles)
	for _, f := range autoFiles {
		vars, err := LoadTfvars(f)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", f, err)
		}
		maps.Copy(result, vars)
	}

	return result, nil
}

// LocalDefinition represents a single local value declaration.
type LocalDefinition struct {
	Name       string
	Expression hcl.Expression
}

// ParseLocals parses all locals blocks from .tf files in a directory.
// Returns individual local value definitions with their HCL expressions.
func ParseLocals(dir string) ([]LocalDefinition, error) {
	files, err := parseTFFiles(dir)
	if err != nil {
		return nil, err
	}

	var locals []LocalDefinition
	for _, f := range files {
		body, ok := f.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}
		for _, block := range body.Blocks {
			if block.Type != "locals" {
				continue
			}
			attrs, diags := block.Body.JustAttributes()
			if diags.HasErrors() {
				continue
			}
			for name, attr := range attrs {
				locals = append(locals, LocalDefinition{Name: name, Expression: attr.Expr})
			}
		}
	}
	return locals, nil
}

// ParseResourceBlockAttrs parses the resource block for a specific type.name
// and returns the attribute names found in the block body.
// This is used as a fallback to discover attribute shapes for zero-instance resources.
func ParseResourceBlockAttrs(dir, resourceType, resourceName string) ([]string, error) {
	files, err := parseTFFiles(dir)
	if err != nil {
		return nil, err
	}

	for _, f := range files {
		body, ok := f.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}
		for _, block := range body.Blocks {
			if block.Type == "resource" && len(block.Labels) >= 2 &&
				block.Labels[0] == resourceType && block.Labels[1] == resourceName {
				attrs, _ := block.Body.JustAttributes()
				var names []string
				for name := range attrs {
					names = append(names, name)
				}
				return names, nil
			}
		}
	}
	return nil, nil
}

// parseTFFiles parses all .tf files in a directory.
func parseTFFiles(dir string) ([]*hcl.File, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	if err != nil {
		return nil, fmt.Errorf("globbing .tf files in %s: %w", dir, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no .tf files found in %s", dir)
	}

	parser := hclparse.NewParser()
	var files []*hcl.File
	for _, path := range matches {
		f, diags := parser.ParseHCLFile(path)
		if diags.HasErrors() {
			return nil, fmt.Errorf("parsing %s: %s", path, diags.Error())
		}
		files = append(files, f)
	}
	return files, nil
}
