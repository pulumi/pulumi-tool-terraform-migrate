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
	"encoding/json"
	"strings"
	"testing"

	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	schemashim "github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfshim/schema"
)

// testFieldDef defines a field for a test provider schema.
type testFieldDef struct {
	pulumiName string              // Pulumi camelCase name override (empty = use TerraformToPulumiNameV2)
	default_   any                 // schema default value
	hasDefault bool                // whether to set default
	computed   bool                // Computed flag
	optional   bool                // Optional flag
	required   bool                // Required flag
	asset      *info.AssetTranslation // asset metadata
}

// buildTestProvider creates a minimal ProviderWithMetadata with the specified fields
// for a given Terraform resource type. This allows unit tests to run without loading
// real providers.
func buildTestProvider(t *testing.T, tfType string, fields map[string]testFieldDef) *ProviderWithMetadata {
	t.Helper()

	// Build the shim schema map from field definitions.
	schemaFields := schemashim.SchemaMap{}
	for tfName, fd := range fields {
		s := &schemashim.Schema{
			Optional: fd.optional,
			Required: fd.required,
			Computed: fd.computed,
		}
		if fd.hasDefault {
			s.Default = fd.default_
		}
		schemaFields[tfName] = s.Shim()
	}

	// Build the shim resource and provider.
	res := &schemashim.Resource{Schema: schemaFields}
	resourceMap := schemashim.ResourceMap{tfType: res.Shim()}
	shimProv := &schemashim.Provider{ResourcesMap: resourceMap}

	// Extract provider prefix from the Terraform type (e.g., "aws" from "aws_s3_bucket").
	prefix, _, ok := strings.Cut(tfType, "_")
	if !ok {
		t.Fatalf("buildTestProvider: tfType %q must contain an underscore", tfType)
	}

	// Build info.Schema overrides for fields that have Pulumi name overrides or asset info.
	infoFields := map[string]*info.Schema{}
	for tfName, fd := range fields {
		if fd.pulumiName != "" || fd.asset != nil {
			si := &info.Schema{}
			if fd.pulumiName != "" {
				si.Name = fd.pulumiName
			}
			if fd.asset != nil {
				si.Asset = fd.asset
			}
			infoFields[tfName] = si
		}
	}

	providerInfo := &info.Provider{
		Name: prefix,
		P:    shimProv.Shim(),
		Resources: map[string]*info.Resource{
			tfType: {Fields: infoFields},
		},
	}

	return &ProviderWithMetadata{
		Provider:         providerInfo,
		TerraformAddress: "registry.terraform.io/hashicorp/" + prefix,
	}
}

// buildTestState creates a minimal Pulumi state JSON with a single resource.
func buildTestState(pulumiType, name string, inputs map[string]any) []byte {
	state := map[string]any{
		"version": 3,
		"deployment": map[string]any{
			"resources": []any{
				map[string]any{
					"urn":    "urn:pulumi:dev::proj::" + pulumiType + "::" + name,
					"type":   pulumiType,
					"custom": true,
					"id":     name,
					"parent": "urn:pulumi:dev::proj::pulumi:pulumi:Stack::proj-dev",
					"inputs": inputs,
					"outputs": inputs,
				},
			},
		},
	}

	data, err := json.Marshal(state)
	if err != nil {
		panic("buildTestState: " + err.Error())
	}
	return data
}

// buildTestStateIO creates a Pulumi state JSON with separate inputs and outputs maps,
// mimicking real exported state where inputs and outputs are independent.
func buildTestStateIO(pulumiType, name string, inputs, outputs map[string]any) []byte {
	state := map[string]any{
		"version": 3,
		"deployment": map[string]any{
			"resources": []any{
				map[string]any{
					"urn":     "urn:pulumi:dev::proj::" + pulumiType + "::" + name,
					"type":    pulumiType,
					"custom":  true,
					"id":      name,
					"parent":  "urn:pulumi:dev::proj::pulumi:pulumi:Stack::proj-dev",
					"inputs":  inputs,
					"outputs": outputs,
				},
			},
		},
	}

	data, err := json.Marshal(state)
	if err != nil {
		panic("buildTestStateIO: " + err.Error())
	}
	return data
}
