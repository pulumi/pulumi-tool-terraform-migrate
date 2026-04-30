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
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/vendored/opentofu/configs/configschema"
	"github.com/pulumi/opentofu/states/statefile"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/vendored/opentofu/providers"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tfprovider"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

// mockProvider implements tfprovider.Provider with controllable schema and upgrade behavior.
type mockProvider struct {
	providers.Interface
	schema         providers.GetProviderSchemaResponse
	upgradeFunc    func(context.Context, providers.UpgradeResourceStateRequest) providers.UpgradeResourceStateResponse
	name, version_ string
}

func (m *mockProvider) GetProviderSchema(_ context.Context) providers.GetProviderSchemaResponse {
	return m.schema
}

func (m *mockProvider) UpgradeResourceState(ctx context.Context, req providers.UpgradeResourceStateRequest) providers.UpgradeResourceStateResponse {
	return m.upgradeFunc(ctx, req)
}

func (m *mockProvider) Name() string                  { return m.name }
func (m *mockProvider) Version() string               { return m.version_ }
func (m *mockProvider) Close(_ context.Context) error { return nil }

// randomStringBlock returns a configschema.Block matching random_string schema v2.
func randomStringBlock() *configschema.Block {
	return &configschema.Block{
		Attributes: map[string]*configschema.Attribute{
			"id":               {Type: cty.String, Computed: true},
			"keepers":          {Type: cty.Map(cty.String), Optional: true},
			"length":           {Type: cty.Number, Required: true},
			"lower":            {Type: cty.Bool, Optional: true, Computed: true},
			"min_lower":        {Type: cty.Number, Optional: true},
			"min_numeric":      {Type: cty.Number, Optional: true},
			"min_special":      {Type: cty.Number, Optional: true},
			"min_upper":        {Type: cty.Number, Optional: true},
			"number":           {Type: cty.Bool, Optional: true, Computed: true},
			"numeric":          {Type: cty.Bool, Optional: true, Computed: true},
			"override_special": {Type: cty.String, Optional: true},
			"result":           {Type: cty.String, Computed: true},
			"special":          {Type: cty.Bool, Optional: true, Computed: true},
			"upper":            {Type: cty.Bool, Optional: true, Computed: true},
		},
	}
}

func newMockRandomProvider() *mockProvider {
	block := randomStringBlock()
	return &mockProvider{
		name:    "random",
		version_: "3.6.0",
		schema: providers.GetProviderSchemaResponse{
			ResourceTypes: map[string]providers.Schema{
				"random_string": {
					Version: 2,
					Block:   block,
				},
			},
		},
		upgradeFunc: func(_ context.Context, req providers.UpgradeResourceStateRequest) providers.UpgradeResourceStateResponse {
			// Simulate the v1->v2 upgrade: parse existing attrs and add "numeric" = "number"
			var attrs map[string]any
			if err := json.Unmarshal(req.RawStateJSON, &attrs); err != nil {
				panic("mock: failed to unmarshal state: " + err.Error())
			}

			// The real random provider copies "number" to "numeric" during upgrade
			if _, ok := attrs["numeric"]; !ok {
				if num, ok := attrs["number"]; ok {
					attrs["numeric"] = num
				} else {
					attrs["numeric"] = true
				}
			}

			return providers.UpgradeResourceStateResponse{
				UpgradedState: attrsToRandomStringCtyValue(attrs),
			}
		},
	}
}

func attrsToRandomStringCtyValue(attrs map[string]any) cty.Value {
	getBool := func(key string) cty.Value {
		if v, ok := attrs[key]; ok && v != nil {
			if b, ok := v.(bool); ok {
				return cty.BoolVal(b)
			}
		}
		return cty.False
	}

	getString := func(key string) cty.Value {
		if v, ok := attrs[key]; ok && v != nil {
			if s, ok := v.(string); ok {
				return cty.StringVal(s)
			}
		}
		return cty.NullVal(cty.String)
	}

	getNumber := func(key string) cty.Value {
		if v, ok := attrs[key]; ok && v != nil {
			switch n := v.(type) {
			case float64:
				return cty.NumberIntVal(int64(n))
			}
		}
		return cty.NumberIntVal(0)
	}

	return cty.ObjectVal(map[string]cty.Value{
		"id":               getString("id"),
		"keepers":          cty.NullVal(cty.Map(cty.String)),
		"length":           getNumber("length"),
		"lower":            getBool("lower"),
		"min_lower":        getNumber("min_lower"),
		"min_numeric":      getNumber("min_numeric"),
		"min_special":      getNumber("min_special"),
		"min_upper":        getNumber("min_upper"),
		"number":           getBool("number"),
		"numeric":          getBool("numeric"),
		"override_special": getString("override_special"),
		"result":           getString("result"),
		"special":          getBool("special"),
		"upper":            getBool("upper"),
	})
}

func mockProviderFactory(mock *mockProvider) ProviderFactory {
	return func(_ context.Context, _, _ string) (tfprovider.Provider, error) {
		return mock, nil
	}
}

func TestUpgradeInstance(t *testing.T) {
	mock := newMockRandomProvider()

	t.Run("random_string_v1_to_v2", func(t *testing.T) {
		tfStateBytes, err := os.ReadFile("testdata/random_string_v1/terraform.tfstate")
		require.NoError(t, err)

		sf, err := statefile.Read(bytes.NewReader(tfStateBytes), encryption.StateEncryptionDisabled())
		require.NoError(t, err)

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

		upgrader := NewStateUpgraderWithFactory(
			map[string]string{"registry.terraform.io/hashicorp/random": "3.6.0"},
			mockProviderFactory(mock),
		)
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
		tfStateBytes, err := os.ReadFile("testdata/random_string_v2/terraform.tfstate")
		require.NoError(t, err)

		sf, err := statefile.Read(bytes.NewReader(tfStateBytes), encryption.StateEncryptionDisabled())
		require.NoError(t, err)

		module := sf.State.Modules[""]
		res := module.Resources["random_string.current"]
		require.NotNil(t, res)
		require.Equal(t, uint64(2), res.Instances[nil].Current.SchemaVersion)

		upgrader := NewStateUpgraderWithFactory(
			map[string]string{"registry.terraform.io/hashicorp/random": "3.6.0"},
			mockProviderFactory(mock),
		)
		defer upgrader.Close()

		// Should return nil (no upgrade needed) since already at current version
		upgraded, err := upgrader.UpgradeInstance(context.Background(), res, nil)
		require.NoError(t, err)
		require.Nil(t, upgraded, "no upgrade needed for current schema version")
	})
}
