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
	"strings"
	"time"
)

// FetchRegistryVersions returns the set of upstream provider versions (without the "v"
// prefix) that the Terraform registry actually serves for the given bridged provider.
// It returns ok=false when the provider has no registry.terraform.io mapping or the
// registry could not be queried; callers should skip validation in that case rather
// than treat it as "no versions available".
//
// Used in internal offline tooling only.
func FetchRegistryVersions(bp BridgedProvider) (map[string]bool, bool) {
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
