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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseVersionFromCommitMessage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		message  string
		expected string
	}{
		{
			name: "Standard upgrade message with v prefix",
			message: `Upgrade terraform-provider-random to v3.8.0

Upgrade terraform-provider-random to v3.8.0 (#1983)
This PR was generated via $ upgrade-provider pulumi/pulumi-random
--kind=provider --target-bridge-version=latest --target-version=3.8.0
--allow-missing-docs=true.`,
			expected: "v3.8.0",
		},
		{
			name:     "Version without v prefix",
			message:  "Upgrade terraform-provider-aws to 5.70.3",
			expected: "5.70.3",
		},
		{
			name:     "Version with v prefix in middle of message",
			message:  "Update provider to use v1.2.3 for compatibility",
			expected: "v1.2.3",
		},
		{
			name:     "Pre-release version",
			message:  "Upgrade to v2.0.0-beta.1 for testing",
			expected: "v2.0.0-beta.1",
		},
		{
			name:     "Version with build metadata",
			message:  "Deploy version 1.0.0+20130313144700",
			expected: "1.0.0+20130313144700",
		},
		{
			name:     "No version in message",
			message:  "Fix bug in provider configuration",
			expected: "",
		},
		{
			name:     "Empty message",
			message:  "",
			expected: "",
		},
		{
			name:     "Multiple versions - returns first",
			message:  "Upgrade from v1.0.0 to v2.0.0",
			expected: "v1.0.0",
		},
		{
			name:     "Version in PR number should not match",
			message:  "Some change (#123)",
			expected: "",
		},
		{
			name:     "Major.minor only should not match",
			message:  "Update to 1.2 version",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseVersionFromCommitMessage(tt.message)
			assert.Equal(t, tt.expected, result)
		})
	}
}
