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
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/pulumi/opentofu/encryption"
	"github.com/pulumi/opentofu/states/statefile"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/providermap"
	"github.com/stretchr/testify/require"
)

func TestUpgradeInstance(t *testing.T) {
	t.Run("random_string_v1_to_v2", func(t *testing.T) {
		// Load v1 state which has schema_version=1 and lacks the "numeric" field
		tfStateBytes, err := os.ReadFile("testdata/random_string_v1/terraform.tfstate")
		require.NoError(t, err)

		sf, err := statefile.Read(bytes.NewReader(tfStateBytes), encryption.StateEncryptionDisabled())
		require.NoError(t, err)

		// Get the resource and instance
		require.Len(t, sf.State.Modules, 1)
		module := sf.State.Modules[""]
		require.NotNil(t, module)

		res := module.Resources["random_string.legacy"]
		require.NotNil(t, res)
		require.Equal(t, uint64(1), res.Instances[nil].Current.SchemaVersion)

		// Verify original state doesn't have "numeric" set
		var originalAttrs map[string]any
		err = json.Unmarshal(res.Instances[nil].Current.AttrsJSON, &originalAttrs)
		require.NoError(t, err)
		require.Nil(t, originalAttrs["numeric"], "original state should not have numeric field")

		// Get the TF provider version for random
		tfAddr := providermap.TerraformProviderName("registry.terraform.io/hashicorp/random")
		version, ok := providermap.GetUpstreamVersion(tfAddr, "")
		require.True(t, ok, "should find random provider version")

		// Create upgrader and upgrade the instance
		upgrader := NewStateUpgrader(map[string]string{
			"registry.terraform.io/hashicorp/random": version,
		})
		defer upgrader.Close()

		upgraded, err := upgrader.UpgradeInstance(context.Background(), res, nil)
		require.NoError(t, err)
		require.NotNil(t, upgraded, "upgrade should return non-nil instance")

		// Verify upgraded state has "numeric" set to true
		var upgradedAttrs map[string]any
		err = json.Unmarshal(upgraded.AttrsJSON, &upgradedAttrs)
		require.NoError(t, err)
		require.Equal(t, true, upgradedAttrs["numeric"], "upgraded state should have numeric=true")

		// Schema version should be updated
		require.Equal(t, uint64(2), upgraded.SchemaVersion)
	})

	t.Run("no_upgrade_needed", func(t *testing.T) {
		// Load v2 state which is already at the current schema version
		tfStateBytes, err := os.ReadFile("testdata/random_string_v2/terraform.tfstate")
		require.NoError(t, err)

		sf, err := statefile.Read(bytes.NewReader(tfStateBytes), encryption.StateEncryptionDisabled())
		require.NoError(t, err)

		module := sf.State.Modules[""]
		res := module.Resources["random_string.current"]
		require.NotNil(t, res)
		require.Equal(t, uint64(2), res.Instances[nil].Current.SchemaVersion)

		// Get the TF provider version for random
		tfAddr := providermap.TerraformProviderName("registry.terraform.io/hashicorp/random")
		version, ok := providermap.GetUpstreamVersion(tfAddr, "")
		require.True(t, ok)

		upgrader := NewStateUpgrader(map[string]string{
			"registry.terraform.io/hashicorp/random": version,
		})
		defer upgrader.Close()

		// Should return nil (no upgrade needed) since already at current version
		upgraded, err := upgrader.UpgradeInstance(context.Background(), res, nil)
		require.NoError(t, err)
		require.Nil(t, upgraded, "no upgrade needed for current schema version")
	})
}
