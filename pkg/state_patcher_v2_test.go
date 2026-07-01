package pkg

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to build a FieldsFile using the flat v2 JSON format (no not_read wrapper).
func fieldsFileFromJSON(t *testing.T, jsonStr string) *FieldsFile {
	t.Helper()
	var ff FieldsFile
	require.NoError(t, json.Unmarshal([]byte(jsonStr), &ff))
	return &ff
}

func TestPatchState_V2FieldsFile_FlatFormat(t *testing.T) {
	t.Parallel()

	ff := fieldsFileFromJSON(t, `{
		"fields": {
			"aws:s3/bucket:Bucket": {
				"forceDestroy": { "default": false }
			}
		}
	}`)

	// Verify parsing worked — NotRead should be populated via UnmarshalJSON.
	require.Len(t, ff.Fields, 1)
	cat := ff.Fields["aws:s3/bucket:Bucket"]
	require.Len(t, cat.NotRead, 1)
	assert.Equal(t, false, cat.NotRead["forceDestroy"].Default)
}

func TestPatchState_V2FieldsFile_AssetField(t *testing.T) {
	t.Parallel()

	ff := fieldsFileFromJSON(t, `{
		"fields": {
			"aws:lambda/function:Function": {
				"publish": { "default": false },
				"code": { "asset": "FileArchive", "assetKind": 2, "archiveFormat": 3, "hashField": "source_code_hash" }
			}
		}
	}`)

	cat := ff.Fields["aws:lambda/function:Function"]
	require.Len(t, cat.NotRead, 2)
	assert.Equal(t, false, cat.NotRead["publish"].Default)
	assert.Equal(t, "FileArchive", cat.NotRead["code"].Asset)
	require.NotNil(t, cat.NotRead["code"].AssetKind)
	assert.Equal(t, 2, *cat.NotRead["code"].AssetKind)
	require.NotNil(t, cat.NotRead["code"].ArchiveFormat)
	assert.Equal(t, 3, *cat.NotRead["code"].ArchiveFormat)
	assert.Equal(t, "source_code_hash", cat.NotRead["code"].HashField)
}

func TestPatchState_V2FieldsFile_LoadFromDisk(t *testing.T) {
	t.Parallel()

	ff, err := LoadFieldsFile("../aws-import-diff-fields-v2.json")
	require.NoError(t, err)

	// Verify a few known entries parsed correctly.
	bucket := ff.Fields["aws:s3/bucket:Bucket"]
	require.Contains(t, bucket.NotRead, "forceDestroy")
	assert.Equal(t, false, bucket.NotRead["forceDestroy"].Default)

	lambda := ff.Fields["aws:lambda/function:Function"]
	require.Contains(t, lambda.NotRead, "code")
	assert.Equal(t, "FileArchive", lambda.NotRead["code"].Asset)
}

func TestPatchState_V2_FileAsset_BridgeRecover(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "swagger-ui", "index.html")
	require.NoError(t, os.MkdirAll(filepath.Dir(testFile), 0o755))
	require.NoError(t, os.WriteFile(testFile, []byte("<html>hello</html>"), 0o644))

	h := sha256.New()
	h.Write([]byte("<html>hello</html>"))
	expectedHash := hex.EncodeToString(h.Sum(nil))

	ff := fieldsFileFromJSON(t, `{
		"fields": {
			"aws:s3/bucketObject:BucketObject": {
				"source": { "asset": "FileAsset", "assetKind": 0 },
				"forceDestroy": { "default": false },
				"acl": { "default": "private" }
			}
		}
	}`)

	stateData := buildTestStateIO("aws:s3/bucketObject:BucketObject", "my-obj",
		map[string]any{"source": "swagger-ui/index.html", "bucket": "my-bucket"},
		map[string]any{
			"source": "swagger-ui/index.html", "bucket": "my-bucket",
			"__pulumi_raw_state_delta": map[string]any{"obj": map[string]any{"ps": map[string]any{}}},
		},
	)

	digest := &ModuleMap{
		RootResources: []ModuleResource{{
			Mode: "managed", TranslatedURN: "urn:pulumi:dev::proj::aws:s3/bucketObject:BucketObject::my-obj",
			TerraformAddress: "aws_s3_object.my_obj",
			Attributes:       map[string]interface{}{"source": "swagger-ui/index.html", "bucket": "my-bucket"},
		}},
	}

	patched, result, err := PatchState(stateData, digest, ff, nil, map[string]string{"aws_s3_object.my_obj": "my-obj"}, nil, tmpDir)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, result.Patched, 1)

	var patchedState map[string]interface{}
	require.NoError(t, json.Unmarshal(patched, &patchedState))
	resources := patchedState["deployment"].(map[string]interface{})["resources"].([]interface{})
	inputs := resources[0].(map[string]interface{})["inputs"].(map[string]interface{})
	outputs := resources[0].(map[string]interface{})["outputs"].(map[string]interface{})

	inputSentinel, ok := inputs["source"].(map[string]interface{})
	require.True(t, ok, "input source should be asset sentinel")
	assert.Equal(t, assetSig, inputSentinel[sigKey])
	assert.Equal(t, expectedHash, inputSentinel["hash"])

	outputSentinel, ok := outputs["source"].(map[string]interface{})
	require.True(t, ok, "output source should be asset sentinel")
	assert.Equal(t, assetSig, outputSentinel[sigKey])

	validatePatchedOutputsAgainstDelta(t, patched)
}

func TestPatchState_V2_FileArchive_BridgeRecover(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	deployDir := filepath.Join(tmpDir, "deploy")
	require.NoError(t, os.MkdirAll(deployDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(deployDir, "handler.py"), []byte("def handler(): pass"), 0o644))

	ff := fieldsFileFromJSON(t, `{
		"fields": {
			"aws:lambda/function:Function": {
				"code": { "asset": "FileArchive", "assetKind": 2, "archiveFormat": 3, "hashField": "source_code_hash" },
				"publish": { "default": false }
			}
		}
	}`)

	stateData := buildTestStateIO("aws:lambda/function:Function", "my-fn",
		map[string]any{"code": "deploy.zip", "functionName": "my-fn", "role": "arn:aws:iam::123:role/test"},
		map[string]any{
			"code": "deploy.zip", "functionName": "my-fn", "role": "arn:aws:iam::123:role/test",
			"__pulumi_raw_state_delta": map[string]any{
				"obj": map[string]any{
					"ps":      map[string]any{},
					"renamed": map[string]any{"code": "filename", "name": "function_name"},
				},
			},
		},
	)

	digest := &ModuleMap{
		RootResources: []ModuleResource{{
			Mode: "managed", TranslatedURN: "urn:pulumi:dev::proj::aws:lambda/function:Function::my-fn",
			TerraformAddress: "aws_lambda_function.my_fn",
			Attributes:       map[string]interface{}{"filename": "deploy.zip", "function_name": "my-fn", "role": "arn:aws:iam::123:role/test", "source_code_hash": "abc123hash"},
		}},
	}

	patched, result, err := PatchState(stateData, digest, ff, nil, map[string]string{"aws_lambda_function.my_fn": "my-fn"}, nil, tmpDir)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, result.Patched, 1)

	var patchedState map[string]interface{}
	require.NoError(t, json.Unmarshal(patched, &patchedState))
	resources := patchedState["deployment"].(map[string]interface{})["resources"].([]interface{})
	inputs := resources[0].(map[string]interface{})["inputs"].(map[string]interface{})
	outputs := resources[0].(map[string]interface{})["outputs"].(map[string]interface{})

	inputCode, ok := inputs["code"].(map[string]interface{})
	require.True(t, ok, "input code should be archive sentinel")
	assert.Equal(t, archiveSig, inputCode[sigKey])

	outputCode, ok := outputs["code"].(map[string]interface{})
	require.True(t, ok, "output code should be archive sentinel")
	assert.Equal(t, archiveSig, outputCode[sigKey])

	delta := outputs["__pulumi_raw_state_delta"].(map[string]interface{})
	ps := delta["obj"].(map[string]interface{})["ps"].(map[string]interface{})
	codeDelta := ps["code"].(map[string]interface{})
	assetEntry := codeDelta["asset"].(map[string]interface{})
	assert.EqualValues(t, 2, assetEntry["kind"])

	validatePatchedOutputsAgainstDelta(t, patched)
}

func TestPatchState_V2_DefaultApplication_BridgeRecover(t *testing.T) {
	t.Parallel()

	ff := fieldsFileFromJSON(t, `{
		"fields": {
			"aws:s3/bucketObject:BucketObject": {
				"forceDestroy": { "default": false },
				"acl": { "default": "private" }
			}
		}
	}`)

	stateData := buildTestStateIO("aws:s3/bucketObject:BucketObject", "my-obj",
		map[string]any{"bucket": "my-bucket", "key": "test.txt"},
		map[string]any{
			"bucket": "my-bucket", "key": "test.txt",
			"__pulumi_raw_state_delta": map[string]any{"obj": map[string]any{"ps": map[string]any{}}},
		},
	)

	digest := &ModuleMap{
		RootResources: []ModuleResource{{
			Mode: "managed", TranslatedURN: "urn:pulumi:dev::proj::aws:s3/bucketObject:BucketObject::my-obj",
			TerraformAddress: "aws_s3_object.my_obj",
			Attributes:       map[string]interface{}{"bucket": "my-bucket", "key": "test.txt"},
		}},
	}

	patched, result, err := PatchState(stateData, digest, ff, nil, map[string]string{"aws_s3_object.my_obj": "my-obj"}, nil, "")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, result.FieldsFromDefaults, 1)

	var patchedState map[string]interface{}
	require.NoError(t, json.Unmarshal(patched, &patchedState))
	resources := patchedState["deployment"].(map[string]interface{})["resources"].([]interface{})
	inputs := resources[0].(map[string]interface{})["inputs"].(map[string]interface{})

	assert.Equal(t, false, inputs["forceDestroy"])
	assert.Equal(t, "private", inputs["acl"])

	validatePatchedOutputsAgainstDelta(t, patched)
}

func TestPatchState_V2_RecoverFailure_RevertsOutputs(t *testing.T) {
	t.Parallel()

	ff := fieldsFileFromJSON(t, `{
		"fields": {
			"aws:s3/bucketObject:BucketObject": {
				"acl": { "default": "private" }
			}
		}
	}`)

	stateData := buildTestStateIO("aws:s3/bucketObject:BucketObject", "my-obj",
		map[string]any{"bucket": "my-bucket", "key": "test.txt"},
		map[string]any{
			"bucket": "my-bucket", "key": "test.txt", "acl": nil,
			"__pulumi_raw_state_delta": map[string]any{
				"obj": map[string]any{
					"ps": map[string]any{
						"acl": map[string]any{
							"plu": map[string]any{
								"i": map[string]any{"obj": map[string]any{"ps": map[string]any{}}},
							},
						},
					},
				},
			},
		},
	)

	digest := &ModuleMap{
		RootResources: []ModuleResource{{
			Mode: "managed", TranslatedURN: "urn:pulumi:dev::proj::aws:s3/bucketObject:BucketObject::my-obj",
			TerraformAddress: "aws_s3_object.my_obj",
			Attributes:       map[string]interface{}{"bucket": "my-bucket", "key": "test.txt", "acl": "private"},
		}},
	}

	patched, result, err := PatchState(stateData, digest, ff, nil, map[string]string{"aws_s3_object.my_obj": "my-obj"}, nil, "")
	require.NoError(t, err)

	assert.Equal(t, 1, result.DeltaFailed, "expected 1 delta failure")
	assert.Equal(t, 0, result.DeltaValidated, "expected 0 delta validated")

	var patchedState map[string]interface{}
	require.NoError(t, json.Unmarshal(patched, &patchedState))
	resources := patchedState["deployment"].(map[string]interface{})["resources"].([]interface{})
	rMap := resources[0].(map[string]interface{})
	outputs := rMap["outputs"].(map[string]interface{})
	inputs := rMap["inputs"].(map[string]interface{})

	assert.Nil(t, outputs["acl"], "acl output should be reverted to nil")
	_, hasDelta := outputs["__pulumi_raw_state_delta"]
	assert.True(t, hasDelta, "delta should still be present after revert")
	// Inputs are also reverted to keep state consistent.
	assert.Nil(t, inputs["acl"], "acl input should also be reverted")
}

// Suppress unused import warnings.
var _ = info.FileAsset
var _ = resource.ZIPArchive
