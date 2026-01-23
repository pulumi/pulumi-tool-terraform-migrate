// Copyright 2016-2026, Pulumi Corporation.
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

package providermap

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/blang/semver/v4"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/contract"
	"gopkg.in/yaml.v3"
)

type BridgedProvider string

type ReleaseTag string

// Semver parses the ReleaseTag as a semver.Version.
func (t ReleaseTag) Semver() semver.Version {
	v, err := semver.Parse(strings.TrimPrefix(string(t), "v"))
	contract.AssertNoErrorf(err, "ReleaseTag should parse as a semver.Version")
	return v
}

func normalizeReleaseTag(tag string) ReleaseTag {
	return ReleaseTag("v" + strings.TrimPrefix(tag, "v"))
}

type VersionPair struct {
	Pulumi   ReleaseTag `yaml:"pulumi"`
	Upstream ReleaseTag `yaml:"upstream,omitempty"`
	Error    string     `yaml:"error,omitempty"`
}

type VersionMap struct {
	Bridged map[BridgedProvider][]VersionPair `yaml:"bridged"`
}

// HasPulumiVersion checks if a Pulumi version already exists for a provider.
func (vm *VersionMap) HasPulumiVersion(bp BridgedProvider, tag ReleaseTag) bool {
	for _, vp := range vm.Bridged[bp] {
		if vp.Pulumi == tag {
			return true
		}
	}
	return false
}

// AddVersion adds a version pair and maintains newest-first ordering by Pulumi version.
// If an entry with the same Pulumi ReleaseTag already exists, it will be replaced.
func (vm *VersionMap) AddVersion(bp BridgedProvider, pulumi, upstream ReleaseTag) {
	if vm.Bridged == nil {
		vm.Bridged = make(map[BridgedProvider][]VersionPair)
	}
	// Remove any existing entry with the same Pulumi ReleaseTag
	vm.removeByPulumiTag(bp, pulumi)
	vm.Bridged[bp] = append(vm.Bridged[bp], VersionPair{Pulumi: pulumi, Upstream: upstream})
	vm.sortVersions(bp)
}

// AddError records a failed version resolution with the error message.
// If an entry with the same Pulumi ReleaseTag already exists, it will be replaced.
func (vm *VersionMap) AddError(bp BridgedProvider, pulumi ReleaseTag, errMsg string) {
	if vm.Bridged == nil {
		vm.Bridged = make(map[BridgedProvider][]VersionPair)
	}
	// Remove any existing entry with the same Pulumi ReleaseTag
	vm.removeByPulumiTag(bp, pulumi)
	vm.Bridged[bp] = append(vm.Bridged[bp], VersionPair{Pulumi: pulumi, Error: errMsg})
	vm.sortVersions(bp)
}

// removeByPulumiTag removes any existing entry with the given Pulumi ReleaseTag.
func (vm *VersionMap) removeByPulumiTag(bp BridgedProvider, pulumi ReleaseTag) {
	versions := vm.Bridged[bp]
	filtered := make([]VersionPair, 0, len(versions))
	for _, vp := range versions {
		if vp.Pulumi != pulumi {
			filtered = append(filtered, vp)
		}
	}
	vm.Bridged[bp] = filtered
}

// sortVersions sorts versions for a provider newest-first by Pulumi version.
func (vm *VersionMap) sortVersions(bp BridgedProvider) {
	sort.Slice(vm.Bridged[bp], func(i, j int) bool {
		// Newest first: i should come before j if i > j
		return vm.Bridged[bp][i].Pulumi.Semver().GT(vm.Bridged[bp][j].Pulumi.Semver())
	})
}

// SaveToYAML saves the VersionMap to a YAML file at the specified path.
func (vm *VersionMap) SaveToYAML(path string) error {
	data, err := yaml.Marshal(vm)
	if err != nil {
		return fmt.Errorf("failed to marshal VersionMap: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write YAML file: %w", err)
	}
	return nil
}

// LoadVersionMapFromYAML loads a VersionMap from a YAML file at the specified path.
func LoadVersionMapFromYAML(path string) (*VersionMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read YAML file: %w", err)
	}
	var vm VersionMap
	if err := yaml.Unmarshal(data, &vm); err != nil {
		return nil, fmt.Errorf("failed to unmarshal VersionMap: %w", err)
	}
	return &vm, nil
}
