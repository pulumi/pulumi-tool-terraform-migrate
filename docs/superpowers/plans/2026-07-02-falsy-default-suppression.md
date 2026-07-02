# Falsy Default Suppression Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Skip patching falsy TF schema defaults when the provider version suppresses them, and consolidate fields files to v2 flat format with background docs in markdown.

**Architecture:** Add a `falsyDefaultSuppression` map to the fields file JSON, build a provider version map from state during `PatchState`, and filter out falsy-default fields when the provider version meets the threshold. Remove the v1 verbose fields file and extract its documentation to a markdown reference doc.

**Tech Stack:** Go, JSON, `encoding/json`, `strconv`, `strings`

**Spec:** `docs/superpowers/specs/2026-07-02-falsy-default-suppression-design.md`

---

## File Map

- **Modify:** `pkg/state_patcher.go` — add `FalsyDefaultSuppression` to `FieldsFile`, add helpers, modify `PatchState` patching loop
- **Modify:** `pkg/state_patcher_v2_test.go` — add unit tests for helpers and integration tests for suppression, update `LoadFieldsFile` test path
- **Modify:** `aws-import-diff-fields.json` — replace with v2 flat format, add `falsyDefaultSuppression`, add new field entries from research
- **Delete:** `aws-import-diff-fields-v2.json` — consolidated into `aws-import-diff-fields.json`
- **Create:** `docs/aws-import-diff-fields.md` — background documentation extracted from v1 JSON
- **Modify:** `cmd/patch_state.go` — print new stat

---

### Task 1: Add helper functions and unit tests

**Files:**
- Modify: `pkg/state_patcher.go`
- Modify: `pkg/state_patcher_v2_test.go`

- [ ] **Step 1: Write failing tests for `isFalsyDefault`**

Add to `pkg/state_patcher_v2_test.go`:

```go
func TestIsFalsyDefault(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		value    interface{}
		expected bool
	}{
		{"bool false", false, true},
		{"bool true", true, false},
		{"float64 zero", float64(0), true},
		{"float64 nonzero", float64(30), false},
		{"json.Number zero", json.Number("0"), true},
		{"json.Number nonzero", json.Number("1"), false},
		{"string empty", "", true},
		{"string nonempty", "private", false},
		{"nil", nil, false},
		{"map", map[string]interface{}{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, isFalsyDefault(tc.value))
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go test ./pkg/ -run TestIsFalsyDefault -v`
Expected: FAIL — `isFalsyDefault` undefined

- [ ] **Step 3: Implement `isFalsyDefault`**

Add to `pkg/state_patcher.go` (after the `shortPulumiType` function, around line 157):

```go
// isFalsyDefault returns true if the default value is falsy, matching
// the bridge's shouldSuppressTFSchemaDefaultValue behavior (bridge >= v3.127.0).
// Falsy defaults: false, 0, "". Nil is not falsy (it means no default).
func isFalsyDefault(v interface{}) bool {
	switch dv := v.(type) {
	case bool:
		return !dv
	case float64:
		return dv == 0
	case json.Number:
		f, err := dv.Float64()
		return err == nil && f == 0
	case string:
		return dv == ""
	default:
		return false
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go test ./pkg/ -run TestIsFalsyDefault -v`
Expected: PASS

- [ ] **Step 5: Write failing tests for `semverAtLeast`**

Add to `pkg/state_patcher_v2_test.go`:

```go
func TestSemverAtLeast(t *testing.T) {
	t.Parallel()
	tests := []struct {
		version, minimum string
		expected         bool
	}{
		{"7.27.0", "7.27.0", true},
		{"7.34.0", "7.27.0", true},
		{"7.26.0", "7.27.0", false},
		{"8.0.0", "7.27.0", true},
		{"6.99.99", "7.27.0", false},
		{"7.27.1", "7.27.0", true},
		{"", "7.27.0", false},
		{"invalid", "7.27.0", false},
		{"7.27.0", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.version+"_>=_"+tc.minimum, func(t *testing.T) {
			assert.Equal(t, tc.expected, semverAtLeast(tc.version, tc.minimum))
		})
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go test ./pkg/ -run TestSemverAtLeast -v`
Expected: FAIL — `semverAtLeast` undefined

- [ ] **Step 7: Implement `semverAtLeast`**

Add to `pkg/state_patcher.go` (after `isFalsyDefault`):

```go
// semverAtLeast returns true if version >= minimum.
// Both must be "major.minor.patch" format. Returns false for malformed input.
func semverAtLeast(version, minimum string) bool {
	parse := func(s string) (int, int, int, bool) {
		parts := strings.SplitN(s, ".", 3)
		if len(parts) != 3 {
			return 0, 0, 0, false
		}
		major, err1 := strconv.Atoi(parts[0])
		minor, err2 := strconv.Atoi(parts[1])
		patch, err3 := strconv.Atoi(parts[2])
		if err1 != nil || err2 != nil || err3 != nil {
			return 0, 0, 0, false
		}
		return major, minor, patch, true
	}

	vMaj, vMin, vPat, vOk := parse(version)
	mMaj, mMin, mPat, mOk := parse(minimum)
	if !vOk || !mOk {
		return false
	}

	if vMaj != mMaj {
		return vMaj > mMaj
	}
	if vMin != mMin {
		return vMin > mMin
	}
	return vPat >= mPat
}
```

Note: add `"strconv"` to the import block in `state_patcher.go`.

- [ ] **Step 8: Run test to verify it passes**

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go test ./pkg/ -run TestSemverAtLeast -v`
Expected: PASS

- [ ] **Step 9: Write failing tests for `providerPackage`**

Add to `pkg/state_patcher_v2_test.go`:

```go
func TestProviderPackage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		resourceType, expected string
	}{
		{"aws:s3/bucket:Bucket", "aws"},
		{"aws:lambda/function:Function", "aws"},
		{"gcp:compute/instance:Instance", "gcp"},
		{"pulumi:pulumi:Stack", "pulumi"},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.resourceType, func(t *testing.T) {
			assert.Equal(t, tc.expected, providerPackage(tc.resourceType))
		})
	}
}
```

- [ ] **Step 10: Run test to verify it fails**

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go test ./pkg/ -run TestProviderPackage -v`
Expected: FAIL — `providerPackage` undefined

- [ ] **Step 11: Implement `providerPackage` and `resolveProviderVersion`**

Add to `pkg/state_patcher.go` (after `semverAtLeast`):

```go
// providerPackage extracts the provider package name from a Pulumi resource type token.
// "aws:s3/bucket:Bucket" → "aws"
func providerPackage(resourceType string) string {
	if idx := strings.IndexByte(resourceType, ':'); idx > 0 {
		return resourceType[:idx]
	}
	return ""
}

// resolveProviderVersion extracts the provider version for a resource.
// The resource's "provider" field is a URN with a trailing UUID:
//
//	urn:pulumi:stack::project::pulumi:providers:aws::name::UUID
//
// We strip the UUID to get the provider URN, then look up the version.
func resolveProviderVersion(providerRef string, providerVersions map[string]string) string {
	if providerRef == "" {
		return ""
	}
	// Strip trailing ::UUID
	if idx := strings.LastIndex(providerRef, "::"); idx > 0 {
		providerURN := providerRef[:idx]
		return providerVersions[providerURN]
	}
	return providerVersions[providerRef]
}
```

- [ ] **Step 12: Run test to verify it passes**

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go test ./pkg/ -run TestProviderPackage -v`
Expected: PASS

- [ ] **Step 13: Commit**

```bash
cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate
git add pkg/state_patcher.go pkg/state_patcher_v2_test.go
git commit -m "feat: add isFalsyDefault, semverAtLeast, providerPackage helpers"
```

---

### Task 2: Add `FalsyDefaultSuppression` to `FieldsFile` and wire into `PatchState`

**Files:**
- Modify: `pkg/state_patcher.go:44-46` (FieldsFile struct)
- Modify: `pkg/state_patcher.go:600-677` (PatchState function)
- Modify: `pkg/state_patcher.go:84-94` (PatchStateResult struct)
- Modify: `cmd/patch_state.go:154-163` (stats output)

- [ ] **Step 1: Add `FalsyDefaultSuppression` to `FieldsFile` struct**

Edit `pkg/state_patcher.go`. The `FieldsFile` struct is at line 44. Add the field:

```go
type FieldsFile struct {
	Fields                  map[string]FieldCategory `json:"fields"`
	FalsyDefaultSuppression map[string]string        `json:"falsyDefaultSuppression,omitempty"`
}
```

- [ ] **Step 2: Add `SkippedFalsySuppressed` to `PatchStateResult`**

Edit `pkg/state_patcher.go`. Add to the `PatchStateResult` struct (line 84):

```go
type PatchStateResult struct {
	Patched                int
	FieldsFromDigest       int
	FieldsFromDefaults     int
	SkippedSensitive       int
	SkippedFalsySuppressed int
	NoMatch                int
	NoFields               int
	DigestMapped           int
	DeltaValidated         int
	DeltaFailed            int
}
```

- [ ] **Step 3: Build provider version map in `PatchState`**

Edit `pkg/state_patcher.go`. Insert the following block at line 599 (after the `notReadByType` loop, before the `// Extract resource info from state for matching` comment):

```go
	// Build provider version map: provider URN → version string.
	providerVersions := make(map[string]string)
	for _, raw := range resourcesRaw {
		rMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		resType, _ := rMap["type"].(string)
		if !strings.HasPrefix(resType, "pulumi:providers:") {
			continue
		}
		urn, _ := rMap["urn"].(string)
		inputs, _ := rMap["inputs"].(map[string]interface{})
		if inputs == nil {
			continue
		}
		version, _ := inputs["version"].(string)
		if version != "" && urn != "" {
			providerVersions[urn] = version
		}
	}
```

- [ ] **Step 4: Filter falsy defaults in the field-descriptor-building loop**

Edit `pkg/state_patcher.go`. Replace the field-descriptor-building loop (lines 664-677) with:

```go
		// Determine if this resource's provider suppresses falsy defaults.
		suppressFalsy := false
		if fieldsFile.FalsyDefaultSuppression != nil {
			pkg := providerPackage(resType)
			if minVersion, ok := fieldsFile.FalsyDefaultSuppression[pkg]; ok {
				providerRef, _ := rMap["provider"].(string)
				provVersion := resolveProviderVersion(providerRef, providerVersions)
				if provVersion != "" && semverAtLeast(provVersion, minVersion) {
					suppressFalsy = true
				}
			}
		}

		// Build field descriptors from the fields file.
		var fields []patchFieldDescriptor
		for pulumiField, meta := range notReadFields {
			if suppressFalsy && meta.Default != nil && isFalsyDefault(meta.Default) {
				result.SkippedFalsySuppressed++
				continue
			}
			fields = append(fields, patchFieldDescriptor{
				PulumiName:    pulumiField,
				TFName:        pulumiToTFField[pulumiField],
				Default:       meta.Default,
				HasDefault:    meta.Default != nil,
				AssetType:     meta.Asset,
				AssetKind:     meta.AssetKind,
				ArchiveFormat: meta.ArchiveFormat,
				HashField:     meta.HashField,
			})
		}
```

- [ ] **Step 5: Print new stat in `cmd/patch_state.go`**

Edit `cmd/patch_state.go`. After the `Skipped sensitive` line (line 160), add:

```go
			if result.SkippedFalsySuppressed > 0 {
				fmt.Fprintf(os.Stderr, "  Skipped falsy suppressed: %d\n", result.SkippedFalsySuppressed)
			}
```

- [ ] **Step 6: Verify compilation**

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go build ./...`
Expected: success

- [ ] **Step 7: Commit**

```bash
cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate
git add pkg/state_patcher.go cmd/patch_state.go
git commit -m "feat: skip falsy defaults when provider version suppresses them"
```

---

### Task 3: Integration tests

**Files:**
- Modify: `pkg/state_patcher_v2_test.go`

- [ ] **Step 1: Write integration tests**

Add to `pkg/state_patcher_v2_test.go`:

```go
func TestPatchState_FalsyDefaultSuppression(t *testing.T) {
	t.Parallel()

	ff := fieldsFileFromJSON(t, `{
		"falsyDefaultSuppression": {
			"aws": "7.27.0"
		},
		"fields": {
			"aws:s3/bucket:Bucket": {
				"forceDestroy": { "default": false }
			},
			"aws:secretsmanager/secret:Secret": {
				"recoveryWindowInDays": { "default": 30 },
				"forceOverwriteReplicaSecret": { "default": false }
			}
		}
	}`)

	// State with AWS provider v7.34.0 (>= 7.27.0 → suppression active).
	stateJSON := `{
		"version": 3,
		"deployment": {
			"resources": [
				{
					"urn": "urn:pulumi:test::proj::pulumi:providers:aws::my-aws",
					"type": "pulumi:providers:aws",
					"custom": true,
					"inputs": { "version": "7.34.0" },
					"outputs": {}
				},
				{
					"urn": "urn:pulumi:test::proj::aws:s3/bucket:Bucket::my-bucket",
					"type": "aws:s3/bucket:Bucket",
					"custom": true,
					"provider": "urn:pulumi:test::proj::pulumi:providers:aws::my-aws::fake-uuid",
					"inputs": {},
					"outputs": {}
				},
				{
					"urn": "urn:pulumi:test::proj::aws:secretsmanager/secret:Secret::my-secret",
					"type": "aws:secretsmanager/secret:Secret",
					"custom": true,
					"provider": "urn:pulumi:test::proj::pulumi:providers:aws::my-aws::fake-uuid",
					"inputs": {},
					"outputs": {}
				}
			]
		}
	}`

	digest := &ModuleMap{}

	patched, result, err := PatchState([]byte(stateJSON), digest, ff, nil, nil, nil, "")
	require.NoError(t, err)

	// forceDestroy (false) and forceOverwriteReplicaSecret (false) should be skipped.
	// recoveryWindowInDays (30) should be patched.
	assert.Equal(t, 2, result.SkippedFalsySuppressed, "should skip 2 falsy defaults")
	assert.Equal(t, 1, result.FieldsFromDefaults, "should patch 1 non-falsy default")

	// Verify the patched state.
	var patchedState map[string]interface{}
	require.NoError(t, json.Unmarshal(patched, &patchedState))
	deployment := patchedState["deployment"].(map[string]interface{})
	resources := deployment["resources"].([]interface{})

	// Bucket: forceDestroy should NOT be in inputs.
	bucket := resources[1].(map[string]interface{})
	bucketInputs := bucket["inputs"].(map[string]interface{})
	assert.NotContains(t, bucketInputs, "forceDestroy", "falsy default should be skipped")

	// Secret: recoveryWindowInDays=30 should be in inputs, forceOverwriteReplicaSecret should NOT.
	secret := resources[2].(map[string]interface{})
	secretInputs := secret["inputs"].(map[string]interface{})
	assert.Contains(t, secretInputs, "recoveryWindowInDays", "non-falsy default should be patched")
	assert.NotContains(t, secretInputs, "forceOverwriteReplicaSecret", "falsy default should be skipped")
}

func TestPatchState_FalsyDefaultSuppression_OldProvider(t *testing.T) {
	t.Parallel()

	ff := fieldsFileFromJSON(t, `{
		"falsyDefaultSuppression": {
			"aws": "7.27.0"
		},
		"fields": {
			"aws:s3/bucket:Bucket": {
				"forceDestroy": { "default": false }
			}
		}
	}`)

	// State with AWS provider v7.26.0 (< 7.27.0 → suppression NOT active).
	stateJSON := `{
		"version": 3,
		"deployment": {
			"resources": [
				{
					"urn": "urn:pulumi:test::proj::pulumi:providers:aws::my-aws",
					"type": "pulumi:providers:aws",
					"custom": true,
					"inputs": { "version": "7.26.0" },
					"outputs": {}
				},
				{
					"urn": "urn:pulumi:test::proj::aws:s3/bucket:Bucket::my-bucket",
					"type": "aws:s3/bucket:Bucket",
					"custom": true,
					"provider": "urn:pulumi:test::proj::pulumi:providers:aws::my-aws::fake-uuid",
					"inputs": {},
					"outputs": {}
				}
			]
		}
	}`

	digest := &ModuleMap{}

	_, result, err := PatchState([]byte(stateJSON), digest, ff, nil, nil, nil, "")
	require.NoError(t, err)

	// With old provider, falsy defaults should still be patched.
	assert.Equal(t, 0, result.SkippedFalsySuppressed)
	assert.Equal(t, 1, result.FieldsFromDefaults, "falsy default should be patched for old provider")
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go test ./pkg/ -run TestPatchState_FalsyDefaultSuppression -v`
Expected: PASS (both tests)

- [ ] **Step 3: Run full test suite**

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go test ./pkg/ -v -count=1`
Expected: all existing tests still pass

- [ ] **Step 4: Commit**

```bash
cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate
git add pkg/state_patcher_v2_test.go
git commit -m "test: add falsy default suppression integration tests"
```

---

### Task 4: Consolidate fields files — replace v1 with v2 flat format

**Files:**
- Rewrite: `aws-import-diff-fields.json` — v2 flat format with `falsyDefaultSuppression` and new field entries
- Delete: `aws-import-diff-fields-v2.json`
- Create: `docs/aws-import-diff-fields.md` — background docs extracted from v1 JSON
- Modify: `pkg/state_patcher_v2_test.go:69` — update `LoadFieldsFile` path

The v1 `aws-import-diff-fields.json` currently contains extensive documentation about categories, root causes, field schemas, and per-field verification metadata. This documentation is valuable but not consumed by the tool — only `default`, `asset`, `assetKind`, `archiveFormat`, and `hashField` are read. Move the docs to markdown; keep the JSON lean.

- [ ] **Step 1: Create `docs/aws-import-diff-fields.md`**

Extract the background info from the current v1 `aws-import-diff-fields.json` into a markdown reference doc. This includes:

- Category definitions (`not_read`, `read_filtered`, `provider_normalized`, `typeset_ordering`, `computed_cascade`, `default_tags_migration`)
- Root cause explanations (`bridge_default`, `aws_api_limitation`, `provider_design`)
- Per-field verification guidance (`sent_to_aws_on_create/update/delete`, `verify.aws_cli`)
- Bridge issue references

Write to `docs/aws-import-diff-fields.md`.

- [ ] **Step 2: Rewrite `aws-import-diff-fields.json` in v2 flat format**

Replace the entire file with v2 flat format. Add `falsyDefaultSuppression`. Add new fields discovered during SGI migration research (from spec). The complete file:

```json
{
  "falsyDefaultSuppression": {
    "aws": "7.27.0"
  },
  "fields": {
    "aws:ecs/service:Service": {
      "waitForSteadyState": { "default": false }
    },
    "aws:ec2/securityGroup:SecurityGroup": {
      "revokeRulesOnDelete": { "default": false }
    },
    "aws:s3/bucketObject:BucketObject": {
      "forceDestroy": { "default": false },
      "content": {},
      "source": { "asset": "FileAsset", "assetKind": 0 },
      "overrideProvider": {}
    },
    "aws:secretsmanager/secretVersion:SecretVersion": {
      "secretString": {}
    },
    "aws:rds/clusterParameterGroup:ClusterParameterGroup": {
      "parameters": {}
    },
    "aws:rds/clusterInstance:ClusterInstance": {
      "applyImmediately": { "default": false },
      "forceDestroy": { "default": false }
    },
    "aws:s3/bucket:Bucket": {
      "forceDestroy": { "default": false }
    },
    "aws:secretsmanager/secret:Secret": {
      "forceOverwriteReplicaSecret": { "default": false },
      "recoveryWindowInDays": { "default": 30 }
    },
    "aws:cloudfront/distribution:Distribution": {
      "retainOnDelete": { "default": false },
      "isIpv6Enabled": { "default": false },
      "staging": { "default": false }
    },
    "aws:acm/certificate:Certificate": {
      "certificateBody": {},
      "certificateChain": {},
      "privateKey": {}
    },
    "aws:sns/topicSubscription:TopicSubscription": {
      "confirmationTimeoutInMinutes": { "default": 1 },
      "endpointAutoConfirms": { "default": false }
    },
    "aws:rds/cluster:Cluster": {
      "applyImmediately": { "default": false },
      "allowMajorVersionUpgrade": {},
      "enableGlobalWriteForwarding": { "default": false },
      "enableLocalWriteForwarding": { "default": false },
      "masterPassword": {},
      "restoreToPointInTime": {},
      "s3Import": {}
    },
    "aws:lambda/function:Function": {
      "publish": { "default": false },
      "skipDestroy": { "default": false },
      "sourceCodeHash": {},
      "code": { "asset": "FileArchive", "assetKind": 2, "archiveFormat": 3, "hashField": "source_code_hash" }
    },
    "aws:lb/listener:Listener": {},
    "aws:ecs/taskDefinition:TaskDefinition": {
      "skipDestroy": { "default": false }
    }
  }
}
```

- [ ] **Step 3: Delete `aws-import-diff-fields-v2.json`**

```bash
cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate
git rm aws-import-diff-fields-v2.json
```

- [ ] **Step 4: Update test that loads from disk**

Edit `pkg/state_patcher_v2_test.go`. Change line 69:

```go
// Before:
ff, err := LoadFieldsFile("../aws-import-diff-fields-v2.json")
// After:
ff, err := LoadFieldsFile("../aws-import-diff-fields.json")
```

- [ ] **Step 5: Run full test suite**

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go test ./pkg/ -v -count=1`
Expected: all tests pass

- [ ] **Step 6: Commit**

```bash
cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate
git add aws-import-diff-fields.json docs/aws-import-diff-fields.md pkg/state_patcher_v2_test.go
git rm aws-import-diff-fields-v2.json
git commit -m "refactor: consolidate fields files to v2 flat format, extract docs to markdown"
```

---

### Task 5: Build, install, and validate against SGI testing stack

**Files:** none (build + validation only)

- [ ] **Step 1: Build and install**

```bash
cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate
go build -o bin/pulumi-terraform-migrate .
pulumi plugin rm tool terraform-migrate --yes
pulumi plugin install tool terraform-migrate 0.3.1 --file bin/pulumi-terraform-migrate
```

- [ ] **Step 2: Re-patch SGI testing state**

```bash
cd /Users/jdavenport/pulumi-repos/veridos/sgi-infrastructure/pulumi
pulumi env run oidc-sgi/nonprod -- pulumi plugin run terraform-migrate -- patch-state \
  --state /tmp/sgi-testing-state.json \
  --digest /tmp/sgi-testing-tf-digest.json \
  --fields /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate/aws-import-diff-fields.json \
  --mapping-file mappings.yaml \
  --project-dir . \
  --stack testing \
  --out /tmp/sgi-testing-state-patched.json
```

Expected: output includes `Skipped falsy suppressed: N` where N > 0 (forceDestroy, applyImmediately, revokeRulesOnDelete, etc.).

- [ ] **Step 3: Import and preview**

```bash
pulumi stack import --stack testing --file /tmp/sgi-testing-state-patched.json
export GITHUB_TOKEN=$(gh auth token) && pulumi preview --stack testing --json > /tmp/preview.json
```

Expected: `forceDestroy` and other falsy-default fields no longer appear in the S3 BucketObject, RDS Cluster, etc. diffs. Total update count should decrease.
