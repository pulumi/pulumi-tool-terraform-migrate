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
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// NewUpstreamVersionValidator returns a function that rejects inferred upstream versions
// the Terraform registry does not actually serve for the given bridged provider (yanked
// releases, or misparsed versions of tools like pulumi-terraform-bridge). The registry
// is queried lazily on first use, so providers with nothing to infer never hit the
// network. If it cannot be queried (or the provider has no registry.terraform.io
// mapping), a warning is printed once and every version is accepted rather than
// failing each one.
//
// Used in internal offline tooling only.
func NewUpstreamVersionValidator(bp BridgedProvider) func(ReleaseTag) error {
	fetch := sync.OnceValues(func() (map[string]bool, bool) {
		available, ok := fetchRegistryVersions(bp)
		if !ok {
			fmt.Fprintf(os.Stderr,
				"  Warning: cannot validate upstream versions for %s against the Terraform registry\n", bp)
		}
		return available, ok
	})
	return func(v ReleaseTag) error {
		available, ok := fetch()
		if ok && !available[strings.TrimPrefix(string(v), "v")] {
			return fmt.Errorf("inferred upstream version %s is not available from the Terraform registry", v)
		}
		return nil
	}
}

// fetchRegistryVersions returns the set of upstream provider versions (without the "v"
// prefix) that the Terraform registry actually serves for the given bridged provider.
// It returns ok=false when the provider has no registry.terraform.io mapping or the
// registry could not be queried. When a provider maps from several registry addresses,
// one successful fetch is enough for ok=true, so a partially failed fetch can yield an
// incomplete set — acceptable because a human reviews the resulting versions.yaml diff.
func fetchRegistryVersions(bp BridgedProvider) (map[string]bool, bool) {
	var addrs []string
	for addr, detail := range providerMapping {
		s := string(addr)
		if detail.pulumiProviderName == string(bp) && strings.HasPrefix(s, "registry.terraform.io/") {
			addrs = append(addrs, strings.TrimPrefix(s, "registry.terraform.io/"))
		}
	}
	if len(addrs) == 0 {
		return nil, false
	}

	client := &http.Client{Timeout: 30 * time.Second}
	available := map[string]bool{}
	ok := false
	for _, a := range addrs {
		versions, err := fetchRegistryVersionList(client, a)
		if err != nil {
			continue
		}
		ok = true
		for _, v := range versions {
			available[v] = true
		}
	}
	return available, ok
}

func fetchRegistryVersionList(client *http.Client, nsName string) ([]string, error) {
	resp, err := client.Get("https://registry.terraform.io/v1/providers/" + nsName + "/versions")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d for %s", resp.StatusCode, nsName)
	}
	var payload struct {
		Versions []struct {
			Version string `json:"version"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(payload.Versions))
	for _, v := range payload.Versions {
		versions = append(versions, v.Version)
	}
	return versions, nil
}
