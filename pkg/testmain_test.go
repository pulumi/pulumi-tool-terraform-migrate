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

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pulumi/opentofu/addrs"
	ottofu "github.com/pulumi/opentofu/tofu"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/providermap"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tofu"
)

// TestMain pre-installs provider plugins before running parallel tests.
// This prevents "text file busy" errors caused by parallel tests trying to
// fork/exec a provider binary while another test is still installing it.
func TestMain(m *testing.M) {
	fixtures, _ := filepath.Glob("testdata/*.json")
	subFixtures, _ := filepath.Glob("testdata/*/*.json")
	fixtures = append(fixtures, subFixtures...)

	// Collect unique provider names across all fixtures.
	ctx := context.Background()
	uniqueProviders := map[providermap.TerraformProviderName]struct{}{}
	for _, fixture := range fixtures {
		tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
			StateFilePath: fixture,
		})
		if err != nil {
			continue
		}
		tofu.VisitResources(tfState, func(r *tfjson.StateResource) error {
			uniqueProviders[providermap.TerraformProviderName(r.ProviderName)] = struct{}{}
			return nil
		}, &tofu.VisitOptions{})
	}

	// Bridge each unique provider once.
	providers := make([]providermap.TerraformProviderName, 0, len(uniqueProviders))
	for name := range uniqueProviders {
		providers = append(providers, name)
	}
	if _, err := PulumiProvidersForTerraformProviders(providers, nil); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: pre-warm failed: %v\n", err)
	}

	// Also pre-warm OpenTofu provider loading (for tests using tofu.Context.Eval).
	// This prevents "text file busy" when parallel tests exec the same provider binary.
	tfDir, err := filepath.Abs(filepath.Join("testdata", "tf_indexed_modules"))
	if err == nil {
		if _, statErr := os.Stat(filepath.Join(tfDir, ".terraform", "providers")); statErr == nil {
			config, configErr := LoadConfig(tfDir)
			if configErr == nil {
				rawState, stateErr := LoadRawState(filepath.Join(tfDir, "terraform.tfstate"))
				if stateErr == nil {
					tofuCtx, cleanup, evalErr := Evaluate(config, rawState, tfDir)
					if evalErr == nil {
						// Run one Eval to fully initialize provider schemas
						_, _ = tofuCtx.Eval(ctx, config, rawState,
							addrs.RootModuleInstance.Child("pet", addrs.IntKey(0)),
							&ottofu.EvalOpts{})
						cleanup()
					}
				}
			}
		}
	}

	os.Exit(m.Run())
}
