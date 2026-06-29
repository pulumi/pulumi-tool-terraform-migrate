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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/sig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// propertyValueFromJSON converts a JSON-deserialized value into a resource.PropertyValue,
// recognizing Pulumi sentinel maps (assets, archives, secrets) and converting them to
// proper typed PropertyValues. This simulates how the engine deserializes state.
func propertyValueFromJSON(v interface{}) resource.PropertyValue {
	replv := func(v interface{}) (resource.PropertyValue, bool) {
		m, ok := v.(map[string]interface{})
		if !ok {
			return resource.PropertyValue{}, false
		}
		s, hasSig := m[sig.Key].(string)
		if !hasSig {
			return resource.PropertyValue{}, false
		}
		switch s {
		case sig.Secret:
			elem := propertyValueFromJSON(m["value"])
			return resource.MakeSecret(elem), true
		default:
			// Asset/archive: use resource.DeserializeAsset/DeserializeArchive
			if a, isAsset, err := resource.DeserializeAsset(m); err == nil && isAsset {
				return resource.NewAssetProperty(a), true
			}
			if ar, isArchive, err := resource.DeserializeArchive(m); err == nil && isArchive {
				return resource.NewArchiveProperty(ar), true
			}
		}
		return resource.PropertyValue{}, false
	}
	return resource.NewPropertyValueRepl(v, nil, replv)
}

// validatePatchedOutputsAgainstDelta reads the patched state JSON and for every resource
// that has a __pulumi_raw_state_delta, validates that each property delta can Recover
// against the corresponding output value.
func validatePatchedOutputsAgainstDelta(t *testing.T, patchedState []byte) {
	t.Helper()

	var state map[string]interface{}
	require.NoError(t, json.Unmarshal(patchedState, &state))

	resources := state["deployment"].(map[string]interface{})["resources"].([]interface{})
	for _, raw := range resources {
		rMap := raw.(map[string]interface{})
		outputs, ok := rMap["outputs"].(map[string]interface{})
		if !ok {
			continue
		}
		deltaRaw, hasDelta := outputs["__pulumi_raw_state_delta"]
		if !hasDelta {
			continue
		}
		deltaMap, ok := deltaRaw.(map[string]interface{})
		if !ok {
			continue
		}
		urn, _ := rMap["urn"].(string)

		// Build the full outputs as a PropertyValue with proper sentinel handling.
		outputsPV := propertyValueFromJSON(outputs)

		// Build the full resource delta and try to recover the full outputs.
		deltaPV := resource.NewPropertyValue(deltaMap)
		rsd, err := tfbridge.UnmarshalRawStateDelta(deltaPV)
		require.NoError(t, err, "UnmarshalRawStateDelta failed for %s", urn)

		_, err = rsd.Recover(outputsPV)
		assert.NoError(t, err, "Recover failed for %s", urn)
	}
}

func TestPatchStateFromSchema_AssetOutputPatched(t *testing.T) {
	t.Parallel()

	// Create a temp file to act as the asset source (S3 object).
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "swagger-ui", "index.html")
	require.NoError(t, os.MkdirAll(filepath.Dir(testFile), 0o755))
	require.NoError(t, os.WriteFile(testFile, []byte("<html>hello</html>"), 0o644))

	h := sha256.New()
	h.Write([]byte("<html>hello</html>"))
	expectedHash := hex.EncodeToString(h.Sum(nil))

	prov := buildTestProvider(t, "aws_s3_object", map[string]testFieldDef{
		"source": {
			optional: true,
			asset:    &info.AssetTranslation{Kind: info.FileAsset},
		},
		"source_hash": {optional: true},
		"bucket":      {required: true},
	})
	providers, typeMap := buildTestProviders(t, prov, map[string]string{
		"aws_s3_object": "aws:s3/object:Object",
	})

	// State with SEPARATE inputs and outputs, both having "source" as a plain string.
	// This mimics real exported state where inputs and outputs are independent maps.
	stateData := buildTestStateIO("aws:s3/object:Object", "my-obj",
		map[string]any{
			"source": "swagger-ui/index.html",
			"bucket": "my-bucket",
		},
		map[string]any{
			"source":     "swagger-ui/index.html",
			"bucket":     "my-bucket",
			"sourceHash": "",
			// Simulate a delta that has an asset entry for "source" (as if import created it).
			"__pulumi_raw_state_delta": map[string]any{
				"obj": map[string]any{
					"ps": map[string]any{},
				},
			},
		},
	)

	digest := &ModuleMap{
		RootResources: []ModuleResource{
			{
				Mode:             "managed",
				TranslatedURN:    "urn:pulumi:dev::proj::aws:s3/object:Object::my-obj",
				TerraformAddress: "aws_s3_object.my_obj",
				Attributes: map[string]interface{}{
					"source": "swagger-ui/index.html",
					"bucket": "my-bucket",
				},
			},
		},
	}

	resourceMappings := map[string]string{
		"aws_s3_object.my_obj": "my-obj",
	}

	patched, result, err := PatchStateFromSchema(stateData, digest, providers, typeMap, nil, resourceMappings, nil, tmpDir)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, result.Patched, 1)

	// Parse the patched state.
	var patchedState map[string]interface{}
	require.NoError(t, json.Unmarshal(patched, &patchedState))
	resources := patchedState["deployment"].(map[string]interface{})["resources"].([]interface{})
	rMap := resources[0].(map[string]interface{})
	inputs := rMap["inputs"].(map[string]interface{})
	outputs := rMap["outputs"].(map[string]interface{})

	// Verify INPUT was patched to an asset sentinel.
	inputSentinel, ok := inputs["source"].(map[string]interface{})
	require.True(t, ok, "input source should be an asset sentinel map, got %T: %v", inputs["source"], inputs["source"])
	assert.Equal(t, assetSig, inputSentinel[sigKey])
	assert.Equal(t, expectedHash, inputSentinel["hash"])

	// Verify OUTPUT was ALSO patched to an asset sentinel.
	outputSentinel, ok := outputs["source"].(map[string]interface{})
	require.True(t, ok, "output source should be an asset sentinel map, got %T: %v", outputs["source"], outputs["source"])
	assert.Equal(t, assetSig, outputSentinel[sigKey])
	assert.Equal(t, expectedHash, outputSentinel["hash"])

	// Verify the delta has an asset entry for "source".
	delta := outputs["__pulumi_raw_state_delta"].(map[string]interface{})
	ps := delta["obj"].(map[string]interface{})["ps"].(map[string]interface{})
	sourceDelta, hasSrc := ps["source"]
	require.True(t, hasSrc, "delta should have an asset entry for source")
	srcDeltaMap := sourceDelta.(map[string]interface{})
	_, hasAsset := srcDeltaMap["asset"]
	assert.True(t, hasAsset, "source delta should have 'asset' entry")

	// Validate the patched state passes bridge's Recover validation.
	validatePatchedOutputsAgainstDelta(t, patched)
}

func TestPatchStateFromSchema_ArchiveOutputPatched(t *testing.T) {
	t.Parallel()

	// Create a directory that buildAssetSentinel will find when stripping .zip.
	// For "deploy.zip", it first checks for directory "deploy".
	tmpDir := t.TempDir()
	deployDir := filepath.Join(tmpDir, "deploy")
	require.NoError(t, os.MkdirAll(deployDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(deployDir, "handler.py"), []byte("def handler(): pass"), 0o644))

	prov := buildTestProvider(t, "aws_lambda_function", map[string]testFieldDef{
		"filename": {
			optional: true,
			asset: &info.AssetTranslation{
				Kind:      info.FileArchive,
				Format:    resource.ZIPArchive,
				HashField: "source_code_hash",
			},
			pulumiName: "code",
		},
		"source_code_hash": {optional: true},
		"function_name":    {required: true},
		"role":             {required: true},
		"runtime":          {optional: true},
		"handler":          {optional: true},
	})
	providers, typeMap := buildTestProviders(t, prov, map[string]string{
		"aws_lambda_function": "aws:lambda/function:Function",
	})

	// Separate inputs and outputs — mimics real state after import.
	stateData := buildTestStateIO("aws:lambda/function:Function", "my-fn",
		map[string]any{
			"code":           "deploy.zip",
			"functionName":   "my-fn",
			"role":           "arn:aws:iam::123:role/test",
			"sourceCodeHash": "",
		},
		map[string]any{
			"code":           "deploy.zip",
			"functionName":   "my-fn",
			"role":           "arn:aws:iam::123:role/test",
			"sourceCodeHash": "",
			"__pulumi_raw_state_delta": map[string]any{
				"obj": map[string]any{
					"ps": map[string]any{},
					"renamed": map[string]any{
						"code": "filename",
						"name": "function_name",
					},
				},
			},
		},
	)

	digest := &ModuleMap{
		RootResources: []ModuleResource{
			{
				Mode:             "managed",
				TranslatedURN:    "urn:pulumi:dev::proj::aws:lambda/function:Function::my-fn",
				TerraformAddress: "aws_lambda_function.my_fn",
				Attributes: map[string]interface{}{
					"filename":         "deploy.zip",
					"function_name":    "my-fn",
					"role":             "arn:aws:iam::123:role/test",
					"source_code_hash": "abc123hash",
				},
			},
		},
	}

	resourceMappings := map[string]string{
		"aws_lambda_function.my_fn": "my-fn",
	}

	patched, result, err := PatchStateFromSchema(stateData, digest, providers, typeMap, nil, resourceMappings, nil, tmpDir)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, result.Patched, 1)

	var patchedState map[string]interface{}
	require.NoError(t, json.Unmarshal(patched, &patchedState))
	resources := patchedState["deployment"].(map[string]interface{})["resources"].([]interface{})
	rMap := resources[0].(map[string]interface{})
	inputs := rMap["inputs"].(map[string]interface{})
	outputs := rMap["outputs"].(map[string]interface{})

	// Verify INPUT code was patched to an archive sentinel.
	inputCode, ok := inputs["code"].(map[string]interface{})
	require.True(t, ok, "input code should be an archive sentinel map, got %T: %v", inputs["code"], inputs["code"])
	assert.Equal(t, archiveSig, inputCode[sigKey])

	// Verify OUTPUT code was ALSO patched to an archive sentinel.
	outputCode, ok := outputs["code"].(map[string]interface{})
	require.True(t, ok, "output code should be an archive sentinel map, got %T: %v", outputs["code"], outputs["code"])
	assert.Equal(t, archiveSig, outputCode[sigKey])

	// Verify the delta has an asset entry for "code".
	delta := outputs["__pulumi_raw_state_delta"].(map[string]interface{})
	ps := delta["obj"].(map[string]interface{})["ps"].(map[string]interface{})
	codeDelta, hasCode := ps["code"]
	require.True(t, hasCode, "delta should have an asset entry for code")
	codeDeltaMap := codeDelta.(map[string]interface{})
	assetEntry, hasAsset := codeDeltaMap["asset"]
	require.True(t, hasAsset, "code delta should have 'asset' entry")
	assetMap := assetEntry.(map[string]interface{})
	// FileArchive = 2
	assert.EqualValues(t, 2, assetMap["kind"])

	// Validate the patched state passes bridge's Recover validation.
	validatePatchedOutputsAgainstDelta(t, patched)
}

// TestPatchStateFromSchema_DeltaRecoverAfterPatch tests that a state that already has
// a delta with asset entries (from a previous import) still passes Recover after patching.
func TestPatchStateFromSchema_DeltaRecoverAfterPatch(t *testing.T) {
	t.Parallel()

	// Create test file for asset.
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "index.html")
	require.NoError(t, os.WriteFile(testFile, []byte("<html>test</html>"), 0o644))

	h := sha256.New()
	h.Write([]byte("<html>test</html>"))
	expectedHash := hex.EncodeToString(h.Sum(nil))

	prov := buildTestProvider(t, "aws_s3_object", map[string]testFieldDef{
		"source": {
			optional: true,
			asset:    &info.AssetTranslation{Kind: info.FileAsset},
		},
		"source_hash": {optional: true},
		"bucket":      {required: true},
		"acl":         {optional: true},
	})
	providers, typeMap := buildTestProviders(t, prov, map[string]string{
		"aws_s3_object": "aws:s3/object:Object",
	})

	// State where the input has the plain string path and the output also has it.
	// The delta already exists (from import) but does NOT yet have the asset entry
	// for "source" — that should be added by patch-state.
	stateData := buildTestStateIO("aws:s3/object:Object", "my-obj",
		map[string]any{
			"source": "index.html",
			"bucket": "my-bucket",
		},
		map[string]any{
			"source":     "index.html",
			"bucket":     "my-bucket",
			"sourceHash": "",
			"acl":        nil,
			"__pulumi_raw_state_delta": map[string]any{
				"obj": map[string]any{
					"ps": map[string]any{},
				},
			},
		},
	)

	digest := &ModuleMap{
		RootResources: []ModuleResource{
			{
				Mode:             "managed",
				TranslatedURN:    "urn:pulumi:dev::proj::aws:s3/object:Object::my-obj",
				TerraformAddress: "aws_s3_object.my_obj",
				Attributes: map[string]interface{}{
					"source":      "index.html",
					"bucket":      "my-bucket",
					"source_hash": "abc123",
					"acl":         "private",
				},
			},
		},
	}

	resourceMappings := map[string]string{
		"aws_s3_object.my_obj": "my-obj",
	}

	patched, _, err := PatchStateFromSchema(stateData, digest, providers, typeMap, nil, resourceMappings, nil, tmpDir)
	require.NoError(t, err)

	// Parse and check outputs.
	var patchedState map[string]interface{}
	require.NoError(t, json.Unmarshal(patched, &patchedState))
	resources := patchedState["deployment"].(map[string]interface{})["resources"].([]interface{})
	outputs := resources[0].(map[string]interface{})["outputs"].(map[string]interface{})

	// Output "source" should be asset sentinel.
	outputSentinel, ok := outputs["source"].(map[string]interface{})
	require.True(t, ok, "output source should be asset sentinel, got %T: %v", outputs["source"], outputs["source"])
	assert.Equal(t, assetSig, outputSentinel[sigKey])
	assert.Equal(t, expectedHash, outputSentinel["hash"])

	// Validate with bridge's Recover.
	validatePatchedOutputsAgainstDelta(t, patched)
}
