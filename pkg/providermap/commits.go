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
	"regexp"
)

// versionPattern matches semantic versions with optional 'v' prefix
// Examples: v3.8.0, 3.8.0, v1.2.3-alpha, 1.0.0-beta+build
var versionPattern = regexp.MustCompile(`v?\d+\.\d+\.\d+(?:[-+][a-zA-Z0-9.-]+)?`)

// ParseVersionFromCommitMessage extracts a version string from a commit message.
// It looks for patterns like "Upgrade terraform-provider-random to v3.8.0" and returns
// the version string (e.g., "v3.8.0"). Returns an empty string if no version is found.
func ParseVersionFromCommitMessage(message string) string {
	match := versionPattern.FindString(message)
	return match
}
