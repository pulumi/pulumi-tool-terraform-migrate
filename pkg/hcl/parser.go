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
	"path/filepath"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/typeexpr"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

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
