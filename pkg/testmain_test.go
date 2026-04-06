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

package pkg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tofu"
)

// TestMain pre-installs provider plugins before running parallel tests.
// This prevents "text file busy" errors caused by parallel tests trying to
// fork/exec a provider binary while another test is still installing it.
func TestMain(m *testing.M) {
	// Find all state JSON fixtures and pre-warm their providers.
	fixtures, _ := filepath.Glob("testdata/*.json")
	subFixtures, _ := filepath.Glob("testdata/*/*.json")
	fixtures = append(fixtures, subFixtures...)

	ctx := context.Background()
	seen := map[string]bool{}
	for _, fixture := range fixtures {
		tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
			StateFilePath: fixture,
		})
		if err != nil {
			continue
		}
		// Deduplicate by provider set — no need to load the same providers twice
		providers, err := GetPulumiProvidersForTerraformState(tfState, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: pre-warm failed for %s: %v\n", fixture, err)
			continue
		}
		for name := range providers {
			if !seen[string(name)] {
				seen[string(name)] = true
			}
		}
	}

	os.Exit(m.Run())
}
