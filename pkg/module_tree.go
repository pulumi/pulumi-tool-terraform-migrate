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
