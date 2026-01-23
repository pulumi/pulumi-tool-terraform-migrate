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
		name        string
		message     string
		expected    string
		expectError bool
	}{
		{
			name: "Standard upgrade message with v prefix",
			message: `Upgrade terraform-provider-random to v3.8.0

Upgrade terraform-provider-random to v3.8.0 (#1983)
This PR was generated via $ upgrade-provider pulumi/pulumi-random
--kind=provider --target-bridge-version=latest --target-version=3.8.0
--allow-missing-docs=true.`,
			expected:    "v3.8.0",
			expectError: false,
		},
		{
			name:        "Version without v prefix",
			message:     "Upgrade terraform-provider-aws to 5.70.3",
			expected:    "v5.70.3",
			expectError: false,
		},
		{
			name:        "Pre-release version with terraform prefix",
			message:     "Upgrade terraform-provider-test to v2.0.0-beta.1 for testing",
			expected:    "v2.0.0-beta.1",
			expectError: false,
		},
		{
			name:        "Version with build metadata and terraform prefix",
			message:     "Deploy terraform to 1.0.0+20130313144700",
			expected:    "v1.0.0+20130313144700",
			expectError: false,
		},
		{
			name:        "Upstream prefix with version",
			message:     "Update upstream to v1.5.0",
			expected:    "v1.5.0",
			expectError: false,
		},
		{
			name:        "UPPERCASE terraform prefix (case-insensitive)",
			message:     "Upgrade TERRAFORM provider to v3.0.0",
			expected:    "v3.0.0",
			expectError: false,
		},
		{
			name:        "Mixed case upstream prefix",
			message:     "Update UpStream to 2.1.0",
			expected:    "v2.1.0",
			expectError: false,
		},
		{
			name:        "No version in message",
			message:     "Fix bug in provider configuration",
			expected:    "",
			expectError: true,
		},
		{
			name:        "Empty message",
			message:     "",
			expected:    "",
			expectError: true,
		},
		{
			name:        "Multiple versions with terraform prefix",
			message:     "Upgrade terraform from v1.0.0 to v2.0.0",
			expected:    "v2.0.0",
			expectError: false,
		},
		{
			name:        "Version in PR number should not match",
			message:     "Some change (#123)",
			expected:    "",
			expectError: true,
		},
		{
			name:        "Major.minor only should not match",
			message:     "Update to 1.2 version",
			expected:    "",
			expectError: true,
		},
		{
			name:        "Version without 'to' prefix should not match",
			message:     "Deploy version 1.0.0 for production",
			expected:    "",
			expectError: true,
		},
		{
			name:        "Bridge update without terraform/upstream prefix should not match",
			message:     "Update bridge to 3.47.3",
			expected:    "",
			expectError: true,
		},
		{
			name:        "Generic 'to' with version should not match",
			message:     "Update provider to v1.2.3 for compatibility",
			expected:    "",
			expectError: true,
		},
		{
			name:        "Random 'to' with version should not match",
			message:     "Upgrade to v2.0.0-beta.1 for testing",
			expected:    "",
			expectError: true,
		},
		{
			name:        "'to' without terraform/upstream should not match",
			message:     "Deploy to 1.0.0+20130313144700",
			expected:    "",
			expectError: true,
		},
		{
			name: "Multiple versions - select largest (stable releases)",
			message: `[v7]: Upgrade upstream to v6.0.0 by @corymhall in #5616
[v7]: Upgrade upstream to v6.1.0 by @corymhall in #5642
[v7]: Upgrade terraform-provider-aws to v6.3.0 by @corymhall in #5654`,
			expected:    "v6.3.0",
			expectError: false,
		},
		{
			name: "Multiple versions - select largest (with pre-releases)",
			message: `[7.0.0-alpha]: upgrade upstream to 6.0.0-beta by @corymhall in #5479
[7.0.0-alpha]: Upgrade upstream to v6.0.0-beta1 by @corymhall in #5511
[v7]: Upgrade upstream to v6.0.0-beta2 by @corymhall in #5571
[v7]: Upgrade upstream to 6.0.0-beta3 by @corymhall in #5606
[v7]: Upgrade upstream to v6.0.0 by @corymhall in #5616`,
			expected:    "v6.0.0",
			expectError: false,
		},
		{
			name: "Multiple versions - stable beats pre-release",
			message: `Upgrade terraform to v5.0.0-rc1
Upgrade terraform to v4.9.0`,
			expected:    "v5.0.0-rc1",
			expectError: false,
		},
		{
			name: "Multiple versions - largest among pre-releases",
			message: `Update upstream to 2.0.0-alpha.1
Update upstream to 2.0.0-beta.1
Update upstream to 2.0.0-alpha.5`,
			expected:    "v2.0.0-beta.1",
			expectError: false,
		},
		{
			name:        "Multiple versions - some with non-terraform/upstream prefix (ignored)",
			message:     "Update bridge to 3.47.3\nUpgrade terraform to v2.0.0\nUpgrade upstream to v2.5.0",
			expected:    "v2.5.0",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseVersionFromCommitMsg(tt.message)
			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, "", string(result))
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, string(result))
			}
		})
	}
}
