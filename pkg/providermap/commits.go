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
)

// versionPattern matches semantic versions in commit messages that follow specific patterns.
// Only matches when the message contains "terraform.*to" or "upstream.*to" (case-insensitive).
// Examples: "Upgrade terraform-provider-aws to v3.8.0", "Update upstream to 1.2.3"
// The version is captured in group 1
var versionPattern = regexp.MustCompile(`(?i)(?:terraform|upstream).*to\s+(v?\d+\.\d+\.\d+(?:[-+][a-zA-Z0-9.-]+)?)`)

// parseVersionFromCommitMsg extracts a version string from a commit message.
// It looks for patterns like "Upgrade terraform-provider-random to v3.8.0" and returns
// the version string (e.g., "v3.8.0"). Returns an error if no version is found.
func parseVersionFromCommitMsg(message string) (string, error) {
	matches := versionPattern.FindStringSubmatch(message)
	if len(matches) < 2 {
		return "", fmt.Errorf("no upstream version found in commit message")
	}
	return matches[1], nil
}
