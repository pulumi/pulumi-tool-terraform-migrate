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
	"sort"
	"strings"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pulumi/opentofu/addrs"
	"github.com/pulumi/opentofu/states"
	"github.com/zclconf/go-cty/cty"
)

// TfjsonFromRawState converts a raw *states.State (parsed from a .tfstate file)
// into a *tfjson.State suitable for the stack translation pipeline.
// This is the inverse of rawStateFromTfjson in generate_module_map.go.
func TfjsonFromRawState(rawState *states.State) *tfjson.State {
	rootModule := &tfjson.StateModule{
		Address: "",
	}

	// Group resources by module path. The root module has an empty path.
	// Child modules are nested under the root.
	moduleMap := map[string]*tfjson.StateModule{
		"": rootModule,
	}

	// Sort module keys for deterministic output.
	moduleKeys := make([]string, 0, len(rawState.Modules))
	for key := range rawState.Modules {
		moduleKeys = append(moduleKeys, key)
	}
	sort.Strings(moduleKeys)

	for _, moduleKey := range moduleKeys {
		module := rawState.Modules[moduleKey]
		moduleAddr := module.Addr.String()

		// Ensure the module exists in our map.
		stateModule, ok := moduleMap[moduleAddr]
		if !ok {
			stateModule = &tfjson.StateModule{
				Address: moduleAddr,
			}
			moduleMap[moduleAddr] = stateModule
		}

		// Sort resource keys for deterministic output.
		resKeys := make([]string, 0, len(module.Resources))
		for key := range module.Resources {
			resKeys = append(resKeys, key)
		}
		sort.Strings(resKeys)

		for _, resKey := range resKeys {
			res := module.Resources[resKey]
			for instKey, inst := range res.Instances {
				if inst.Current == nil {
					continue
				}

				stateResource := rawInstanceToTfjson(module.Addr, res.Addr.Resource, res.ProviderConfig.Provider, instKey, inst.Current)
				stateModule.Resources = append(stateModule.Resources, stateResource)
			}
		}
	}

	// Build the module hierarchy: attach child modules to their parents.
	buildModuleHierarchy(rootModule, moduleMap)

	return &tfjson.State{
		FormatVersion: "1.0",
		Values: &tfjson.StateValues{
			RootModule: rootModule,
		},
	}
}

// rawInstanceToTfjson converts a single resource instance from raw state to tfjson format.
func rawInstanceToTfjson(
	moduleAddr addrs.ModuleInstance,
	resAddr addrs.Resource,
	provider addrs.Provider,
	instKey addrs.InstanceKey,
	obj *states.ResourceInstanceObjectSrc,
) *tfjson.StateResource {
	// Build the full address string.
	address := buildResourceAddress(moduleAddr, resAddr, instKey)

	// Determine mode.
	mode := tfjson.ManagedResourceMode
	if resAddr.Mode == addrs.DataResourceMode {
		mode = tfjson.DataResourceMode
	}

	// Unmarshal attribute values from JSON.
	var attrValues map[string]interface{}
	if len(obj.AttrsJSON) > 0 {
		if err := json.Unmarshal(obj.AttrsJSON, &attrValues); err != nil {
			// If we can't parse, use empty map rather than failing.
			attrValues = map[string]interface{}{}
		}
	}

	// Convert sensitive paths to the nested SensitiveValues format.
	var sensitiveValues json.RawMessage
	if len(obj.AttrSensitivePaths) > 0 {
		sv := pathValueMarksToObj(obj.AttrSensitivePaths)
		if data, err := json.Marshal(sv); err == nil {
			sensitiveValues = data
		}
	}

	// Convert instance key to index.
	var index interface{}
	switch k := instKey.(type) {
	case addrs.IntKey:
		index = int(k)
	case addrs.StringKey:
		index = string(k)
	}

	return &tfjson.StateResource{
		Address:         address,
		Mode:            mode,
		Type:            resAddr.Type,
		Name:            resAddr.Name,
		Index:           index,
		ProviderName:    provider.String(),
		SchemaVersion:   obj.SchemaVersion,
		AttributeValues: attrValues,
		SensitiveValues: sensitiveValues,
	}
}

// buildResourceAddress constructs the full Terraform address string for a resource instance.
func buildResourceAddress(moduleAddr addrs.ModuleInstance, resAddr addrs.Resource, instKey addrs.InstanceKey) string {
	var parts []string

	// Module path.
	if moduleStr := moduleAddr.String(); moduleStr != "" {
		parts = append(parts, moduleStr)
	}

	// Resource type and name.
	modePrefix := ""
	if resAddr.Mode == addrs.DataResourceMode {
		modePrefix = "data."
	}
	parts = append(parts, fmt.Sprintf("%s%s.%s", modePrefix, resAddr.Type, resAddr.Name))

	address := strings.Join(parts, ".")

	// Instance key suffix.
	switch k := instKey.(type) {
	case addrs.IntKey:
		address = fmt.Sprintf("%s[%d]", address, int(k))
	case addrs.StringKey:
		address = fmt.Sprintf("%s[\"%s\"]", address, string(k))
	}

	return address
}

// pathValueMarksToObj converts cty.PathValueMarks (used by AttrSensitivePaths)
// to the nested JSON structure used by tfjson.StateResource.SensitiveValues.
// In this structure, `true` marks a sensitive leaf, and nested objects mark
// sensitive sub-paths.
func pathValueMarksToObj(pvms []cty.PathValueMarks) interface{} {
	root := make(map[string]interface{})

	for _, pvm := range pvms {
		current := root
		steps := pvm.Path
		for i, step := range steps {
			isLast := i == len(steps)-1
			switch s := step.(type) {
			case cty.GetAttrStep:
				name := s.Name
				if isLast {
					current[name] = true
				} else {
					if _, ok := current[name]; !ok {
						current[name] = make(map[string]interface{})
					}
					if nested, ok := current[name].(map[string]interface{}); ok {
						current = nested
					}
				}
			case cty.IndexStep:
				// Index steps (array elements) — use the index as a string key.
				key := fmt.Sprintf("%v", s.Key.AsString())
				if isLast {
					current[key] = true
				} else {
					if _, ok := current[key]; !ok {
						current[key] = make(map[string]interface{})
					}
					if nested, ok := current[key].(map[string]interface{}); ok {
						current = nested
					}
				}
			}
		}
	}

	if len(root) == 0 {
		return nil
	}
	return root
}

// buildModuleHierarchy attaches child modules in moduleMap to their parents.
// A module with address "module.foo.module.bar" is a child of "module.foo".
func buildModuleHierarchy(root *tfjson.StateModule, moduleMap map[string]*tfjson.StateModule) {
	// Sort module addresses by depth (shallowest first) so parents are processed before children.
	addrs := make([]string, 0, len(moduleMap))
	for addr := range moduleMap {
		if addr != "" { // Skip root
			addrs = append(addrs, addr)
		}
	}
	sort.Slice(addrs, func(i, j int) bool {
		// Sort by number of "module." occurrences (depth), then alphabetically.
		di := strings.Count(addrs[i], "module.")
		dj := strings.Count(addrs[j], "module.")
		if di != dj {
			return di < dj
		}
		return addrs[i] < addrs[j]
	})

	for _, addr := range addrs {
		child := moduleMap[addr]
		parentAddr := parentModuleAddress(addr)
		parent, ok := moduleMap[parentAddr]
		if !ok {
			// Parent doesn't exist yet, create it.
			parent = &tfjson.StateModule{
				Address: parentAddr,
			}
			moduleMap[parentAddr] = parent
		}
		parent.ChildModules = append(parent.ChildModules, child)
	}
}

// parentModuleAddress returns the parent module address.
// "module.foo.module.bar" -> "module.foo"
// "module.foo" -> ""
func parentModuleAddress(addr string) string {
	// Find the last "module." segment and remove it.
	lastDotModule := strings.LastIndex(addr, ".module.")
	if lastDotModule >= 0 {
		return addr[:lastDotModule]
	}
	// Top-level module — parent is root.
	return ""
}
