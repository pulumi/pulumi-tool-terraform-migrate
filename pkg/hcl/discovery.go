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
