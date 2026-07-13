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
	"sort"
	"testing"
)

// TestVersionsYAMLNoCrossProviderUpstreamPoisoning is a tripwire for a failure mode where
// the update-providermap inference records a shared dependency's version (historically the
// pulumi-terraform-bridge version, e.g. "Upgrade pulumi-terraform-bridge to v3.130.0") as a
// provider's upstream version. Because such a bump lands across many provider repos at once,
// the poisoned value shows up as the upstream version of many unrelated providers, which
// never happens with genuine upstream versions: as of the 2026-07 cleanup the most widely
// shared legitimate upstream version (v2.4.0) spans 10 providers, while the poisoned
// v3.130.0 spanned 31. If this test fails, inspect the offending entries in versions.yaml
// before considering a threshold bump.
func TestVersionsYAMLNoCrossProviderUpstreamPoisoning(t *testing.T) {
	t.Parallel()
	const maxProvidersPerUpstreamVersion = 12

	providersByUpstream := map[ReleaseTag][]string{}
	for bp, pairs := range refinedVersionMap.Bridged {
		seen := map[ReleaseTag]bool{}
		for _, vp := range pairs {
			if vp.Error != "" || vp.Upstream == "" || seen[vp.Upstream] {
				continue
			}
			seen[vp.Upstream] = true
			providersByUpstream[vp.Upstream] = append(providersByUpstream[vp.Upstream], string(bp))
		}
	}

	for version, providers := range providersByUpstream {
		if len(providers) > maxProvidersPerUpstreamVersion {
			sort.Strings(providers)
			t.Errorf("upstream version %s is recorded for %d different providers (%v); "+
				"this looks like a shared dependency version (e.g. pulumi-terraform-bridge) "+
				"misrecorded as an upstream provider version by update-providermap",
				version, len(providers), providers)
		}
	}
}
