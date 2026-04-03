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
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// moduleSegment represents a single module in a Terraform address path.
type moduleSegment struct {
	name string // e.g., "vpc"
	key  string // e.g., "0" or "us-east-1" for indexed/keyed modules, empty for non-indexed
}

// deriveComponentTypeToken generates a Pulumi type token from a module name.
// Example: "s3_bucket" -> "terraform:module/s3Bucket:S3Bucket"
func deriveComponentTypeToken(moduleName string) string {
	parts := strings.Split(moduleName, "_")

	// PascalCase: capitalize first char of each part
	var pascalParts []string
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		runes := []rune(p)
		runes[0] = unicode.ToUpper(runes[0])
		pascalParts = append(pascalParts, string(runes))
	}
	pascalCase := strings.Join(pascalParts, "")

	// camelCase: lowercase first char of PascalCase, but only if there were underscores
	camelCase := moduleName
	if len(parts) > 1 {
		runes := []rune(pascalCase)
		runes[0] = unicode.ToLower(runes[0])
		camelCase = string(runes)
	}

	return fmt.Sprintf("terraform:module/%s:%s", camelCase, pascalCase)
}

// sanitizeModuleInstanceName creates a resource name for a keyed/indexed module instance.
// Example: ("vpc", "us-east-1") -> "vpc-us-east-1"
func sanitizeModuleInstanceName(moduleName, key string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	sanitized := re.ReplaceAllString(key, "-")
	sanitized = strings.Trim(sanitized, "-")
	return moduleName + "-" + sanitized
}

// moduleIndexRegex matches module indices like [0] or ["us-east-1"]
var moduleIndexRegex = regexp.MustCompile(`^([^\[]+)\[(?:"([^"]+)"|(\d+))\]$`)

// parseModuleSegments extracts module path segments from a Terraform resource address.
// Example: "module.vpc.module.subnets.aws_subnet.this" -> [{name:"vpc"}, {name:"subnets"}]
// Returns nil for root-level resources (no module prefix).
func parseModuleSegments(address string) []moduleSegment {
	parts := strings.Split(address, ".")
	var segments []moduleSegment

	for i := 0; i < len(parts); i++ {
		if parts[i] != "module" {
			break
		}
		if i+1 >= len(parts) {
			break
		}
		i++
		raw := parts[i]

		seg := moduleSegment{}
		if m := moduleIndexRegex.FindStringSubmatch(raw); m != nil {
			seg.name = m[1]
			if m[2] != "" {
				seg.key = m[2] // string key
			} else {
				seg.key = m[3] // numeric index
			}
		} else {
			seg.name = raw
		}
		segments = append(segments, seg)
	}

	return segments
}

// componentNode represents a component resource in the tree.
type componentNode struct {
	name         string           // module name (e.g., "vpc")
	key          string           // index/key if present
	resourceName string           // Pulumi resource name (e.g., "vpc" or "vpc-0")
	typeToken    string           // Pulumi type token
	modulePath   string           // full TF module path (e.g., "module.vpc.module.subnets")
	children     []*componentNode // child components
}

// buildComponentTree constructs a tree of component nodes from TF resource addresses.
// typeOverrides maps TF module paths (e.g., "module.vpc") to Pulumi type tokens.
// Returns error on name/type collisions. Results sorted alphabetically at each level.
func buildComponentTree(resourceAddresses []string, typeOverrides map[string]string) ([]*componentNode, error) {
	type moduleInfo struct {
		segments []moduleSegment
		path     string
	}

	seen := map[string]bool{}
	var allPaths []moduleInfo

	for _, addr := range resourceAddresses {
		segs := parseModuleSegments(addr)
		if segs == nil {
			continue
		}
		// Register all prefix paths for nesting
		for depth := 1; depth <= len(segs); depth++ {
			prefix := make([]moduleSegment, depth)
			copy(prefix, segs[:depth])
			path := buildModulePath(prefix)
			if !seen[path] {
				seen[path] = true
				allPaths = append(allPaths, moduleInfo{segments: prefix, path: path})
			}
		}
	}

	// Sort by depth (shorter paths first), then alphabetically
	sort.Slice(allPaths, func(i, j int) bool {
		if len(allPaths[i].segments) != len(allPaths[j].segments) {
			return len(allPaths[i].segments) < len(allPaths[j].segments)
		}
		return allPaths[i].path < allPaths[j].path
	})

	rootNodes := map[string]*componentNode{}
	allNodes := map[string]*componentNode{}

	for _, info := range allPaths {
		lastSeg := info.segments[len(info.segments)-1]
		resName := lastSeg.name
		if lastSeg.key != "" {
			resName = sanitizeModuleInstanceName(lastSeg.name, lastSeg.key)
		}

		// Determine type token: check override using base path (without index)
		basePath := buildModuleBasePath(info.segments)
		typeToken := deriveComponentTypeToken(lastSeg.name)
		if override, ok := typeOverrides[basePath]; ok {
			typeToken = override
		}

		node := &componentNode{
			name:         lastSeg.name,
			key:          lastSeg.key,
			resourceName: resName,
			typeToken:    typeToken,
			modulePath:   info.path,
		}
		allNodes[info.path] = node

		if len(info.segments) == 1 {
			rootNodes[info.path] = node
		} else {
			parentPath := buildModulePath(info.segments[:len(info.segments)-1])
			if parent, ok := allNodes[parentPath]; ok {
				parent.children = append(parent.children, node)
			}
		}
	}

	// Validate: check for collision on (resourceName, parentPath)
	type nameKey struct{ name, parent string }
	names := map[nameKey]string{} // key -> original module path
	for _, info := range allPaths {
		node := allNodes[info.path]
		parentPath := ""
		if len(info.segments) > 1 {
			parentPath = buildModulePath(info.segments[:len(info.segments)-1])
		}
		key := nameKey{node.resourceName, parentPath}
		if existing, ok := names[key]; ok && existing != info.path {
			return nil, fmt.Errorf("component name collision: %q and %q both produce name %q under the same parent",
				existing, info.path, node.resourceName)
		}
		names[key] = info.path
	}

	// Collect and sort root nodes alphabetically
	var result []*componentNode
	for _, node := range rootNodes {
		result = append(result, node)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].resourceName < result[j].resourceName
	})

	// Sort children at each level
	var sortChildren func(nodes []*componentNode)
	sortChildren = func(nodes []*componentNode) {
		for _, n := range nodes {
			if len(n.children) > 0 {
				sort.Slice(n.children, func(i, j int) bool {
					return n.children[i].resourceName < n.children[j].resourceName
				})
				sortChildren(n.children)
			}
		}
	}
	sortChildren(result)

	return result, nil
}

// buildModulePath constructs a full module path string from segments.
// Example: [{name:"vpc"}, {name:"subnets"}] -> "module.vpc.module.subnets"
func buildModulePath(segments []moduleSegment) string {
	var parts []string
	for _, seg := range segments {
		if seg.key != "" {
			parts = append(parts, fmt.Sprintf("module.%s[%s]", seg.name, formatKey(seg.key)))
		} else {
			parts = append(parts, "module."+seg.name)
		}
	}
	return strings.Join(parts, ".")
}

// buildModuleBasePath constructs a module path without indices/keys for type override matching.
// Example: [{name:"vpc", key:"0"}] -> "module.vpc"
func buildModuleBasePath(segments []moduleSegment) string {
	var parts []string
	for _, seg := range segments {
		parts = append(parts, "module."+seg.name)
	}
	return strings.Join(parts, ".")
}

func formatKey(key string) string {
	if _, err := fmt.Sscanf(key, "%d", new(int)); err == nil {
		return key
	}
	return `"` + key + `"`
}

// toComponents converts the component tree into a flat, depth-first ordered list of PulumiResources.
// parentTypeChain is the $-delimited type chain of the parent (empty for top-level).
func toComponents(nodes []*componentNode, parentTypeChain string) []PulumiResource {
	var result []PulumiResource
	for _, node := range nodes {
		comp := PulumiResource{
			PulumiResourceID: PulumiResourceID{
				Name: node.resourceName,
				Type: node.typeToken,
			},
			Parent: parentTypeChain,
		}
		result = append(result, comp)

		childParentChain := node.typeToken
		if parentTypeChain != "" {
			childParentChain = parentTypeChain + "$" + node.typeToken
		}

		if len(node.children) > 0 {
			result = append(result, toComponents(node.children, childParentChain)...)
		}
	}
	return result
}

// componentParentForResource returns the parent type chain for a resource at the given module path.
func componentParentForResource(nodes []*componentNode, segments []moduleSegment) string {
	if len(segments) == 0 {
		return ""
	}

	current := nodes
	var typeChain string
	for _, seg := range segments {
		targetName := seg.name
		if seg.key != "" {
			targetName = sanitizeModuleInstanceName(seg.name, seg.key)
		}
		found := false
		for _, node := range current {
			if node.resourceName == targetName {
				if typeChain != "" {
					typeChain += "$"
				}
				typeChain += node.typeToken
				current = node.children
				found = true
				break
			}
		}
		if !found {
			return ""
		}
	}
	return typeChain
}
