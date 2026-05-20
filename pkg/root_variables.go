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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/pulumi/opentofu/configs"
	"github.com/pulumi/opentofu/tofu"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tfc"
	"github.com/zclconf/go-cty/cty"
)

// BuildRootVariables constructs tofu.InputValues for all root module variables.
// It merges values from (in increasing priority):
//  1. Variable defaults from the config
//  2. terraform.tfvars (if present)
//  3. *.auto.tfvars (alphabetically)
//  4. Remote workspace variables (from TFC/Scalr)
//
// Any variable still without a value gets cty.UnknownVal of its declared type.
func BuildRootVariables(
	config *configs.Config,
	tfDir string,
	remoteVars []tfc.WorkspaceVariable,
) tofu.InputValues {
	rootVars := make(tofu.InputValues, len(config.Root.Module.Variables))

	// Start with all variables unset (NilVal = use default).
	for name := range config.Root.Module.Variables {
		rootVars[name] = &tofu.InputValue{
			Value:      cty.NilVal,
			SourceType: tofu.ValueFromCaller,
		}
	}

	// Layer 1: terraform.tfvars
	tfvarsPath := filepath.Join(tfDir, "terraform.tfvars")
	if vals, err := parseTFVarsFile(tfvarsPath); err == nil {
		for k, v := range vals {
			if _, declared := config.Root.Module.Variables[k]; declared {
				rootVars[k] = &tofu.InputValue{
					Value:      v,
					SourceType: tofu.ValueFromNamedFile,
				}
			}
		}
	}

	// Layer 2: *.auto.tfvars (alphabetically)
	autoFiles, _ := filepath.Glob(filepath.Join(tfDir, "*.auto.tfvars"))
	sort.Strings(autoFiles)
	for _, f := range autoFiles {
		if vals, err := parseTFVarsFile(f); err == nil {
			for k, v := range vals {
				if _, declared := config.Root.Module.Variables[k]; declared {
					rootVars[k] = &tofu.InputValue{
						Value:      v,
						SourceType: tofu.ValueFromAutoFile,
					}
				}
			}
		}
	}

	// Layer 3: Remote workspace variables (highest priority for non-sensitive).
	for _, rv := range remoteVars {
		if _, declared := config.Root.Module.Variables[rv.Key]; !declared {
			continue
		}
		if rv.Sensitive {
			// Sensitive vars come back empty from the API; use unknown.
			v := config.Root.Module.Variables[rv.Key]
			ty := v.Type
			if ty == cty.NilType {
				ty = cty.DynamicPseudoType
			}
			rootVars[rv.Key] = &tofu.InputValue{
				Value:      cty.UnknownVal(ty),
				SourceType: tofu.ValueFromCaller,
			}
			continue
		}
		if rv.HCL {
			if val, err := parseHCLExpression(rv.Value); err == nil {
				rootVars[rv.Key] = &tofu.InputValue{
					Value:      val,
					SourceType: tofu.ValueFromCaller,
				}
			}
		} else {
			rootVars[rv.Key] = &tofu.InputValue{
				Value:      cty.StringVal(rv.Value),
				SourceType: tofu.ValueFromCaller,
			}
		}
	}

	// Fill remaining unset required variables with unknown values.
	for name, v := range config.Root.Module.Variables {
		iv := rootVars[name]
		if iv.Value != cty.NilVal {
			continue
		}
		// NilVal means "use default" — only supply unknown if no default exists.
		if v.Default == cty.NilVal {
			ty := v.Type
			if ty == cty.NilType {
				ty = cty.DynamicPseudoType
			}
			rootVars[name] = &tofu.InputValue{
				Value:      cty.UnknownVal(ty),
				SourceType: tofu.ValueFromCaller,
			}
		}
	}

	return rootVars
}

// parseTFVarsFile parses an HCL .tfvars file and returns the attribute values.
func parseTFVarsFile(path string) (map[string]cty.Value, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	file, diags := hclsyntax.ParseConfig(data, path, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing %s: %s", path, diags.Error())
	}

	attrs, diags := file.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("reading attributes from %s: %s", path, diags.Error())
	}

	result := make(map[string]cty.Value, len(attrs))
	for name, attr := range attrs {
		val, valDiags := attr.Expr.Value(nil)
		if valDiags.HasErrors() {
			continue
		}
		result[name] = val
	}
	return result, nil
}

// parseHCLExpression parses an HCL expression string (for HCL-typed workspace variables).
func parseHCLExpression(expr string) (cty.Value, error) {
	// Wrap in a synthetic attribute assignment so we can parse it.
	src := fmt.Sprintf("v = %s", strings.TrimSpace(expr))
	file, diags := hclsyntax.ParseConfig([]byte(src), "<remote-var>", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return cty.NilVal, fmt.Errorf("parsing HCL expression: %s", diags.Error())
	}

	attrs, diags := file.Body.JustAttributes()
	if diags.HasErrors() {
		return cty.NilVal, fmt.Errorf("reading HCL expression: %s", diags.Error())
	}

	val, valDiags := attrs["v"].Expr.Value(nil)
	if valDiags.HasErrors() {
		return cty.NilVal, fmt.Errorf("evaluating HCL expression: %s", valDiags.Error())
	}
	return val, nil
}
