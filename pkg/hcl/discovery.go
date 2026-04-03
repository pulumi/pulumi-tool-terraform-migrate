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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DiscoverModuleSources parses root .tf files and extracts local module source paths.
// Remote sources (registry, git) are skipped — they require explicit mapping via
// CLI flag or migration file. Terragrunt users must also provide explicit mappings.
//
// Returns a map of "module.<name>" -> source path for local modules only.
func DiscoverModuleSources(rootDir string) (map[string]string, error) {
	callSites, err := ParseModuleCallSites(rootDir)
	if err != nil {
		return nil, err
	}

	sources := map[string]string{}
	for _, cs := range callSites {
		if IsLocalModuleSource(cs.Source) {
			sources["module."+cs.Name] = cs.Source
		}
	}
	return sources, nil
}

// IsLocalModuleSource returns true if the source is a local path (starts with "./" or "../" or "/").
func IsLocalModuleSource(source string) bool {
	return len(source) > 0 && (source[0] == '.' || source[0] == '/')
}

// moduleManifest represents the .terraform/modules/modules.json structure.
type moduleManifest struct {
	Modules []moduleManifestEntry `json:"Modules"`
}

type moduleManifestEntry struct {
	Key    string `json:"Key"`
	Source string `json:"Source"`
	Dir    string `json:"Dir"`
}

// ResolveModuleSourcesFromCache reads .terraform/modules/modules.json and returns
// a map of "module.<name>" -> directory path for each cached module.
// This resolves remote modules (registry, git) that tofu init has downloaded.
// Returns empty map if no cache exists (not an error).
func ResolveModuleSourcesFromCache(rootDir string) (map[string]string, error) {
	manifestPath := filepath.Join(rootDir, ".terraform", "modules", "modules.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return map[string]string{}, nil
	}

	var manifest moduleManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing modules.json: %w", err)
	}

	sources := map[string]string{}
	for _, entry := range manifest.Modules {
		if entry.Key == "" {
			continue
		}
		moduleAddr := manifestKeyToModuleAddr(entry.Key)
		dir := entry.Dir
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(rootDir, dir)
		}
		sources[moduleAddr] = dir
	}
	return sources, nil
}

// manifestKeyToModuleAddr converts a modules.json key like "rdsdb.db_subnet_group"
// to a TF module address like "module.rdsdb.module.db_subnet_group".
func manifestKeyToModuleAddr(key string) string {
	parts := strings.Split(key, ".")
	var addr []string
	for _, p := range parts {
		addr = append(addr, "module."+p)
	}
	return strings.Join(addr, ".")
}
