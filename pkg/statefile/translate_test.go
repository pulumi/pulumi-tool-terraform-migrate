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

package statefile

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/hexops/autogold/v2"
	"github.com/pulumi/opentofu/encryption"
	"github.com/pulumi/opentofu/states/statefile"
	tfmigrate "github.com/pulumi/pulumi-tool-terraform-migrate/pkg"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/providermap"
	"github.com/stretchr/testify/require"
)

// discoverTestCases finds all terraform.tfstate files in testdata subdirectories.
func discoverTestCases(t *testing.T) []string {
	t.Helper()

	matches, err := filepath.Glob("testdata/*/terraform.tfstate")
	require.NoError(t, err)
	require.NotEmpty(t, matches, "no terraform.tfstate files found in testdata/*/")

	return matches
}

func TestTranslateResource(t *testing.T) {
	t.Parallel()

	for _, tfstatePath := range discoverTestCases(t) {
		testName := filepath.Base(filepath.Dir(tfstatePath))
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			// Read and parse the terraform state file
			tfStateBytes, err := os.ReadFile(tfstatePath)
			require.NoError(t, err)

			sf, err := statefile.Read(bytes.NewReader(tfStateBytes), encryption.StateEncryptionDisabled())
			require.NoError(t, err)

			// Collect provider names and get Pulumi provider mappings
			providerNames := make(map[providermap.TerraformProviderName]struct{})
			for _, module := range sf.State.Modules {
				for _, tfResource := range module.Resources {
					providerName := tfResource.ProviderConfig.Provider.String()
					providerNames[providermap.TerraformProviderName(providerName)] = struct{}{}
				}
			}

			providers, err := tfmigrate.PulumiProvidersForTerraformProviders(slices.Collect(maps.Keys(providerNames)), nil)
			require.NoError(t, err, "failed to get provider mappings")

			// Translate the entire statefile
			result, err := TranslateStateFile(t.Context(), sf.State, providers)
			require.NoError(t, err, "failed to translate statefile")
			require.Empty(t, result.Skipped, "some resources were skipped: %v", result.Skipped)

			// Compare against golden file
			autogold.ExpectFile(t, result.Resources)
		})
	}
}
