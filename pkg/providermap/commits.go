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
	"regexp"
	"strings"

	"github.com/blang/semver/v4"
)

// versionPattern matches semantic versions in commit messages that follow specific patterns.
// Only matches when the message contains "terraform.*to" or "upstream.*to" (case-insensitive).
// Examples: "Upgrade terraform-provider-aws to v3.8.0", "Update upstream to 1.2.3"
// The version is captured in group 1
var versionPattern = regexp.MustCompile(`(?i)(?:terraform|upstream).*to\s+(v?\d+\.\d+\.\d+(?:[-+][a-zA-Z0-9.-]+)?)`)

// parseVersionFromCommitMsg extracts a version string from a commit message.
// It looks for patterns like "Upgrade terraform-provider-random to v3.8.0" and returns
// the version string (e.g., "v3.8.0"). If multiple versions are found, it returns the
// largest version by semantic versioning rules. Returns an error if no version is found.
func parseVersionFromCommitMsg(message string) (ReleaseTag, error) {
	allMatches := versionPattern.FindAllStringSubmatch(message, -1)
	if len(allMatches) == 0 {
		return "", fmt.Errorf("no upstream version found in commit message")
	}

	// If only one match, return it directly
	if len(allMatches) == 1 {
		return normalizeReleaseTag(allMatches[0][1]), nil
	}

	// Multiple matches - find the largest version
	var maxVersion semver.Version
	var maxVersionStr string

	for _, match := range allMatches {
		versionStr := match[1]

		// Parse the version (strip 'v' prefix if present for parsing)
		versionToParse := strings.TrimPrefix(versionStr, "v")
		version, err := semver.Parse(versionToParse)
		if err != nil {
			// Skip invalid versions
			continue
		}

		// Track the maximum version
		if maxVersionStr == "" || version.GT(maxVersion) {
			maxVersion = version
			maxVersionStr = versionStr
		}
	}

	if maxVersionStr == "" {
		return "", fmt.Errorf("no valid semantic versions found in commit message")
	}

	return normalizeReleaseTag(maxVersionStr), nil
}
