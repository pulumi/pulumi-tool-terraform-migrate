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

package tofu

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// providerBlockRegex matches provider blocks in .terraform.lock.hcl:
//
//	provider "registry.terraform.io/hashicorp/aws" {
//	  version = "5.31.0"
var providerBlockRegex = regexp.MustCompile(
	`provider\s+"([^"]+)"\s*\{[^}]*version\s*=\s*"([^"]+)"`,
)

// GetProviderVersionsFromLockfile parses .terraform.lock.hcl in the given
// directory and returns a map of provider source addresses to their resolved
// versions. This avoids needing to run `tofu version -json`.
//
// Returns an empty map (not an error) if the lockfile does not exist.
func GetProviderVersionsFromLockfile(tfDir string) (map[string]string, error) {
	lockPath := filepath.Join(tfDir, ".terraform.lock.hcl")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("reading lockfile %s: %w", lockPath, err)
	}

	return parseLockfileVersions(string(data)), nil
}

// parseLockfileVersions extracts provider → version mappings from lockfile content.
func parseLockfileVersions(content string) map[string]string {
	versions := map[string]string{}

	// The regex approach works because the lockfile format is machine-generated
	// and highly predictable. Each provider block has exactly one version field.
	matches := providerBlockRegex.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		provider := match[1]
		version := strings.TrimSpace(match[2])
		versions[provider] = version
	}

	return versions
}
