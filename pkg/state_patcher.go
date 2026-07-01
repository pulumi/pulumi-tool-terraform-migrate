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
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/providermap"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	pulumiarchive "github.com/pulumi/pulumi/sdk/v3/go/common/resource/archive"
	pulumiasset "github.com/pulumi/pulumi/sdk/v3/go/common/resource/asset"
)

// FieldsFile represents the patch-state fields file.
// The JSON format is flat: { "fields": { "aws:s3/bucket:Bucket": { "forceDestroy": { "default": false } } } }
// In Go tests, FieldCategory (which wraps NotRead) is still supported for convenience.
type FieldsFile struct {
	Fields map[string]FieldCategory `json:"fields"`
}

// FieldCategory groups fields for a resource type.
// JSON files use the flat format (no "not_read" wrapper); Go test code may use NotRead.
type FieldCategory struct {
	NotRead map[string]FieldInfo `json:"not_read,omitempty"`
}

// UnmarshalJSON supports both flat format (v2) and nested format (v1 with not_read).
func (fc *FieldCategory) UnmarshalJSON(data []byte) error {
	// Try v1 (nested): { "not_read": { ... } }
	type v1Format struct {
		NotRead map[string]FieldInfo `json:"not_read"`
	}
	var v1 v1Format
	if err := json.Unmarshal(data, &v1); err == nil && len(v1.NotRead) > 0 {
		fc.NotRead = v1.NotRead
		return nil
	}
	// Try v2 (flat): { "fieldName": { ... } }
	var v2 map[string]FieldInfo
	if err := json.Unmarshal(data, &v2); err == nil {
		fc.NotRead = v2
		return nil
	}
	return fmt.Errorf("cannot parse field category")
}

// FieldInfo describes a single field to patch.
type FieldInfo struct {
	Default       interface{} `json:"default"`
	Asset         string      `json:"asset,omitempty"`         // "FileAsset" or "FileArchive"
	AssetKind     *int        `json:"assetKind,omitempty"`     // bridge AssetTranslationKind (0=FileAsset, 2=FileArchive)
	ArchiveFormat *int        `json:"archiveFormat,omitempty"` // resource.ArchiveFormat (3=ZIPArchive)
	HashField     string      `json:"hashField,omitempty"`     // e.g. "source_code_hash"
}

// PatchStateResult contains statistics from the patch operation.
type PatchStateResult struct {
	Patched          int
	FieldsFromDigest int
	FieldsFromDefaults int
	SkippedSensitive int
	NoMatch          int
	NoFields         int
	DigestMapped     int
	DeltaValidated   int // resources whose delta passed Recover validation
	DeltaFailed      int // resources whose delta failed Recover (outputs reverted)
}

// patchFieldDescriptor describes a single field to consider for patching.
// Both fields-based and schema-based paths build these, then delegate to
// the shared patchResourceFields function.
type patchFieldDescriptor struct {
	PulumiName    string
	TFName        string
	Default       interface{} // nil means no default
	HasDefault    bool
	AssetType     string // "FileAsset", "FileArchive", or ""
	AssetKind     *int
	ArchiveFormat *int
	HashField     string
}

// tfToPulumiField maps TF snake_case attribute names to Pulumi camelCase field names
// for known not_read fields.
var tfToPulumiField = map[string]string{
	"acl":                                "acl",
	"apply_immediately":                  "applyImmediately",
	"certificate_body":                   "certificateBody",
	"certificate_chain":                  "certificateChain",
	"filename":                           "code",
	"confirmation_timeout_in_minutes":    "confirmationTimeoutInMinutes",
	"content":                            "content",
	"endpoint_auto_confirms":             "endpointAutoConfirms",
	"force_destroy":                      "forceDestroy",
	"force_detach_policies":              "forceDetachPolicies",
	"force_overwrite_replica_secret":     "forceOverwriteReplicaSecret",
	"master_password":                    "masterPassword",
	"parameter":                          "parameters",
	"private_key":                        "privateKey",
	"publish":                            "publish",
	"recovery_window_in_days":            "recoveryWindowInDays",
	"revoke_rules_on_delete":             "revokeRulesOnDelete",
	"secret_string":                      "secretString",
	"skip_destroy":                       "skipDestroy",
	"source":                             "source",
	"wait_for_steady_state":              "waitForSteadyState",
}

// pulumiToTFField is the reverse of tfToPulumiField.
var pulumiToTFField = func() map[string]string {
	m := make(map[string]string, len(tfToPulumiField))
	for k, v := range tfToPulumiField {
		m[v] = k
	}
	return m
}()

// shortPulumiType extracts the short type from a full Pulumi type token.
// "aws:secretsmanager/secret:Secret" → "secret:Secret"
// "pulumi:pulumi:Stack" → "pulumi:Stack"
func shortPulumiType(fullType string) string {
	parts := strings.FieldsFunc(fullType, func(r rune) bool {
		return r == ':' || r == '/'
	})
	if len(parts) >= 3 {
		return parts[len(parts)-2] + ":" + parts[len(parts)-1]
	}
	return ""
}

// BuildDigestNameMap builds a mapping from Pulumi resource name → ModuleResource
// using the same matching logic as FillImportFile: resource-level mappings first,
// then module-level type+name matching with type-only fallback.
func BuildDigestNameMap(
	digest *ModuleMap,
	moduleMappings, resourceMappings map[string]string,
	stateResources []json.RawMessage,
	stateResourceNames map[string]stateResourceInfo,
) map[string]*ModuleResource {
	result := make(map[string]*ModuleResource)

	// Index all managed digest resources by TF address.
	tfByAddress := map[string]*ModuleResource{}
	for i := range digest.RootResources {
		r := &digest.RootResources[i]
		if r.Mode == "managed" {
			tfByAddress[r.TerraformAddress] = r
		}
	}
	collectAllResources(digest.Modules, tfByAddress)

	// Phase 1: Resource-level mappings (direct).
	for tfAddr, pulumiName := range resourceMappings {
		if r, ok := tfByAddress[tfAddr]; ok {
			result[pulumiName] = r
		}
	}

	// Phase 2: Module-level matching.
	tfByModule := map[string][]ModuleResource{}
	collectModuleResources(digest.Modules, tfByModule)

	for tfModulePath, componentName := range moduleMappings {
		tfResources, ok := tfByModule[tfModulePath]
		if !ok {
			continue
		}

		// Find state resources that are children of this component.
		var children []stateResourceInfo
		for _, info := range stateResourceNames {
			if info.parentName == componentName {
				children = append(children, info)
			}
		}

		// Index TF resources by [type, name] for matching.
		type typeNameKey struct{ pulumiType, tfName string }
		byTypeName := map[typeNameKey]*ModuleResource{}
		byType := map[string][]*ModuleResource{}

		for i := range tfResources {
			r := &tfResources[i]
			if r.Mode != "managed" {
				continue
			}
			pulumiType := extractTypeFromURN(r.TranslatedURN)
			if pulumiType == "" {
				continue
			}
			tfName := extractResourceName(r.TerraformAddress)
			byTypeName[typeNameKey{pulumiType, tfName}] = r
			byType[pulumiType] = append(byType[pulumiType], r)
		}

		used := map[string]bool{}
		for _, child := range children {
			if _, already := result[child.name]; already {
				continue
			}

			suffix := extractImportSuffix(child.name, componentName)

			// Try exact match by type + name.
			key := typeNameKey{child.resourceType, suffix}
			if r, ok := byTypeName[key]; ok && !used[r.TerraformAddress] {
				result[child.name] = r
				used[r.TerraformAddress] = true
				continue
			}

			// Try normalized match: strip "this[" wrapper and quotes from TF name,
			// since component children often have TF names like this["key"]
			// while Pulumi suffix is just "key".
			matched := false
			for tkKey, r := range byTypeName {
				if tkKey.pulumiType != child.resourceType || used[r.TerraformAddress] {
					continue
				}
				normalized := normalizeTFName(tkKey.tfName)
				if normalized == suffix {
					result[child.name] = r
					used[r.TerraformAddress] = true
					matched = true
					break
				}
			}
			if matched {
				continue
			}

			// Fallback: exactly one unused candidate of this type.
			var candidates []*ModuleResource
			for _, r := range byType[child.resourceType] {
				if !used[r.TerraformAddress] {
					candidates = append(candidates, r)
				}
			}
			if len(candidates) == 1 {
				result[child.name] = candidates[0]
				used[candidates[0].TerraformAddress] = true
			}
		}
	}

	// Phase 3: Root resources (parented to Stack).
	{
		type typeNameKey struct{ pulumiType, tfName string }
		byTypeName := map[typeNameKey]*ModuleResource{}
		byType := map[string][]*ModuleResource{}

		for i := range digest.RootResources {
			r := &digest.RootResources[i]
			if r.Mode != "managed" {
				continue
			}
			pulumiType := extractTypeFromURN(r.TranslatedURN)
			if pulumiType == "" {
				continue
			}
			tfName := extractResourceName(r.TerraformAddress)
			byTypeName[typeNameKey{pulumiType, tfName}] = r
			byType[pulumiType] = append(byType[pulumiType], r)
		}

		used := map[string]bool{}
		for _, info := range stateResourceNames {
			if !info.isRoot {
				continue
			}
			if _, already := result[info.name]; already {
				continue
			}

			key := typeNameKey{info.resourceType, info.name}
			if r, ok := byTypeName[key]; ok && !used[r.TerraformAddress] {
				result[info.name] = r
				used[r.TerraformAddress] = true
				continue
			}

			candidates := make([]*ModuleResource, 0)
			for _, r := range byType[info.resourceType] {
				if !used[r.TerraformAddress] {
					candidates = append(candidates, r)
				}
			}
			if len(candidates) == 1 {
				result[info.name] = candidates[0]
				used[candidates[0].TerraformAddress] = true
			}
		}
	}

	return result
}

// stateResourceInfo holds the minimal info needed from a state resource for matching.
type stateResourceInfo struct {
	name         string
	resourceType string
	parentName   string
	isRoot       bool // parented directly to Stack
}

// patchResourceFieldsResult holds the per-resource patching outcome.
type patchResourceFieldsResult struct {
	patched            bool
	fieldsFromDigest   int
	fieldsFromDefaults int
	skippedSensitive   int
	patchedAssetFields []assetFieldDeltaInfo
	deltaValidated     bool
	deltaFailed        bool
}

// patchResourceFields is the shared patching logic for both fields-based and
// schema-based paths. It iterates the field descriptors, resolves values from
// the digest, builds asset sentinels, resolves secrets, and patches inputs/outputs.
//
// It modifies inputsRaw and outputsRaw in place and returns the result.
func patchResourceFields(
	fields []patchFieldDescriptor,
	inputsRaw, outputsRaw map[string]interface{},
	digResource *ModuleResource,
	configSecrets map[string]string,
	configDir string,
) (*patchResourceFieldsResult, error) {
	res := &patchResourceFieldsResult{}

	for _, fd := range fields {
		pulumiField := fd.PulumiName
		tfAttr := fd.TFName

		// Get digest value if we have a match.
		var digVal interface{}
		if digResource != nil && tfAttr != "" {
			digVal = digResource.Attributes[tfAttr]
			switch digVal.(type) {
			case []interface{}, map[string]interface{}:
				digVal = camelCaseKeys(digVal)
			}
		}

		inputVal := inputsRaw[pulumiField]
		inputEmpty := isEmptyValue(inputVal)
		outputVal := outputsRaw[pulumiField]
		outputEmpty := isEmptyValue(outputVal)

		digSensitive := digVal == "(sensitive)"
		digEmpty := digVal == nil || digVal == "" || digSensitive

		// For asset fields, convert TF source path to Pulumi asset sentinel.
		if fd.AssetType != "" && configDir != "" && !digEmpty && !digSensitive {
			if pathStr, ok := digVal.(string); ok {
				absPath := filepath.Join(configDir, pathStr)
				if _, err := os.Stat(absPath); err != nil && filepath.IsAbs(pathStr) {
					base := strings.TrimSuffix(filepath.Base(pathStr), ".zip")
					absPath = filepath.Join(configDir, base)
				}
				sentinel, err := buildAssetSentinel(absPath, fd.AssetType)
				if err != nil && fd.AssetType == "FileArchive" && digResource != nil {
					if fnName, ok := digResource.Attributes["function_name"].(string); ok && fnName != "" {
						fnArn, _ := digResource.Attributes["arn"].(string)
						fmt.Fprintf(os.Stderr, "  Downloading Lambda code for %s (%s)...\n", pulumiField, fnName)
						sentinel, err = downloadLambdaCodeAsArchive(fnName, fnArn)
					}
				}
				if err != nil {
					fmt.Fprintf(os.Stderr, "  WARNING: could not build asset sentinel for %s (%s): %v\n",
						pulumiField, absPath, err)
				} else {
					digVal = sentinel
				}
			}
		}

		// For sensitive fields, resolve from stack config.
		if digSensitive && digResource != nil && tfAttr != "" && len(configSecrets) > 0 {
			configKey := flattenAddress(digResource.TerraformAddress, tfAttr)
			if secretVal, ok := configSecrets[configKey]; ok && secretVal != "" {
				jsonEncoded, err := json.Marshal(secretVal)
				if err != nil {
					return nil, fmt.Errorf("encoding secret value for %s: %w", configKey, err)
				}
				digVal = map[string]interface{}{
					"4dabf18193072939515e22adb298388d": "1b47061264138c4ac30d75fd1eb44270",
					"plaintext":                        string(jsonEncoded),
				}
				digEmpty = false
				digSensitive = false
			}
		}

		isSentinel := isSecretSentinel(digVal)
		inputIsBadPlain := isSentinel && inputVal != nil && !isSecretSentinel(inputVal)

		isAssetSent := isAssetOrArchiveSentinel(digVal)
		inputIsBadAsset := isAssetSent && inputVal != nil && !isAssetOrArchiveSentinel(inputVal)

		// Patch inputs.
		if inputEmpty || inputIsBadPlain || inputIsBadAsset {
			if !digEmpty {
				inputsRaw[pulumiField] = digVal
				res.fieldsFromDigest++
				res.patched = true
				if isAssetSent && fd.AssetKind != nil {
					format := 0
					if fd.ArchiveFormat != nil {
						format = *fd.ArchiveFormat
					}
					res.patchedAssetFields = append(res.patchedAssetFields, assetFieldDeltaInfo{
						pulumiField: pulumiField,
						tfField:     tfAttr,
						kind:        *fd.AssetKind,
						format:      format,
						hashField:   fd.HashField,
					})
				}
			} else if digSensitive {
				res.skippedSensitive++
			} else if fd.HasDefault && fd.Default != nil {
				inputsRaw[pulumiField] = fd.Default
				res.fieldsFromDefaults++
				res.patched = true
			}
		}

		// Patch outputs for simple values and asset sentinels only.
		outputIsBadPlain := isSentinel && outputVal != nil && !isSecretSentinel(outputVal)
		outputIsNullSentinel := isSentinel && isNullSentinel(outputVal)
		outputStale := res.patched && !digEmpty && outputVal != nil && !outputEmpty && !equalValues(outputVal, digVal)
		if outputEmpty || outputIsBadPlain || outputIsNullSentinel || outputStale {
			var newOutputVal interface{}
			if !digEmpty {
				newOutputVal = digVal
			} else if fd.HasDefault && fd.Default != nil {
				newOutputVal = fd.Default
			}
			if newOutputVal != nil {
				rawOutput := unwrapSecretSentinel(newOutputVal)
				if isSimpleValue(rawOutput) || isAssetOrArchiveSentinel(rawOutput) {
					outputsRaw[pulumiField] = rawOutput
				}
			}
		}
	}

	return res, nil
}

// patchAndValidateResource patches a single resource's fields and validates
// the result with the bridge's Recover function. If Recover fails, outputs
// are reverted to their pre-patch state.
//
// Returns the result, the (possibly modified) inputs and outputs.
func patchAndValidateResource(
	urn, name string,
	fields []patchFieldDescriptor,
	inputsRaw, outputsRaw map[string]interface{},
	digResource *ModuleResource,
	configSecrets map[string]string,
	configDir string,
) (*patchResourceFieldsResult, map[string]interface{}, map[string]interface{}, error) {
	// Snapshot inputs and outputs before patching so we can revert on Recover failure.
	inputsSnapshot := make(map[string]interface{}, len(inputsRaw))
	for k, v := range inputsRaw {
		inputsSnapshot[k] = v
	}
	outputsSnapshot := make(map[string]interface{}, len(outputsRaw))
	for k, v := range outputsRaw {
		outputsSnapshot[k] = v
	}

	res, err := patchResourceFields(fields, inputsRaw, outputsRaw, digResource, configSecrets, configDir)
	if err != nil {
		return nil, nil, nil, err
	}

	// Update __pulumi_raw_state_delta for patched asset fields.
	if deltaRaw, hasDelta := outputsRaw["__pulumi_raw_state_delta"]; hasDelta {
		if len(res.patchedAssetFields) > 0 {
			outputsRaw["__pulumi_raw_state_delta"] = injectAssetDeltas(deltaRaw, res.patchedAssetFields)
		}
	}

	// Validate patched outputs against delta using bridge's Recover.
	// If Recover fails, revert both inputs and outputs to pre-patch state
	// to avoid panics and keep inputs/outputs consistent.
	if _, hasDelta := outputsRaw["__pulumi_raw_state_delta"]; hasDelta && res.patched {
		if recoverErr := validateRecover(urn, outputsRaw); recoverErr != nil {
			fmt.Fprintf(os.Stderr, "  WARNING: Recover failed for %s: %v — reverting all patches\n", name, recoverErr)
			inputsRaw = inputsSnapshot
			outputsRaw = outputsSnapshot
			res.deltaFailed = true
			res.patched = false
		} else {
			res.deltaValidated = true
		}
	}

	return res, inputsRaw, outputsRaw, nil
}

// PatchState patches not_read fields from digest into imported state.
// configSecrets is an optional map of config key → decrypted value, used to
// resolve sensitive fields that the digest redacts as "(sensitive)". Keys are
// generated by flattenAddress(terraformAddress, tfAttribute).
// configDir is the TF config directory, used to resolve asset file paths for
// fields with asset type (FileAsset/FileArchive). Can be empty to skip asset patching.
func PatchState(
	stateData []byte,
	digest *ModuleMap,
	fieldsFile *FieldsFile,
	moduleMappings, resourceMappings map[string]string,
	configSecrets map[string]string,
	configDir string,
) ([]byte, *PatchStateResult, error) {
	// Parse state using a decoder with UseNumber to preserve exact number
	// representations. Without this, large integers (like AWS account IDs)
	// become float64 and may re-serialize as scientific notation (e.g.,
	// "5399223e-54"), which Pulumi's state parser rejects.
	var state map[string]interface{}
	dec := json.NewDecoder(strings.NewReader(string(stateData)))
	dec.UseNumber()
	if err := dec.Decode(&state); err != nil {
		return nil, nil, fmt.Errorf("parsing state: %w", err)
	}

	deployment, ok := state["deployment"].(map[string]interface{})
	if !ok {
		return nil, nil, fmt.Errorf("state missing deployment")
	}

	resourcesRaw, ok := deployment["resources"].([]interface{})
	if !ok {
		return nil, nil, fmt.Errorf("state missing resources")
	}

	// fieldMeta holds per-field metadata from the fields file.
	type fieldMeta struct {
		Default       interface{}
		Asset         string // "FileAsset", "FileArchive", or ""
		AssetKind     *int
		ArchiveFormat *int
		HashField     string
	}

	// Build field sets keyed by both full and short type.
	// The fields file uses full type keys (aws:secretsmanager/secret:Secret),
	// but we match state resources by short type (secret:Secret) for convenience.
	notReadByType := map[string]map[string]fieldMeta{} // type → {pulumiField → meta}
	for fullType, cat := range fieldsFile.Fields {
		if len(cat.NotRead) > 0 {
			fields := make(map[string]fieldMeta, len(cat.NotRead))
			for pulumiField, info := range cat.NotRead {
				fields[pulumiField] = fieldMeta{
					Default:       info.Default,
					Asset:         info.Asset,
					AssetKind:     info.AssetKind,
					ArchiveFormat: info.ArchiveFormat,
					HashField:     info.HashField,
				}
			}
			notReadByType[fullType] = fields
			st := shortPulumiType(fullType)
			if st != "" {
				notReadByType[st] = fields
			}
		}
	}

	// Extract resource info from state for matching.
	stateInfos := make(map[string]stateResourceInfo)
	for _, raw := range resourcesRaw {
		rMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		custom, _ := rMap["custom"].(bool)
		if !custom {
			continue
		}
		urn, _ := rMap["urn"].(string)
		resType, _ := rMap["type"].(string)
		parent, _ := rMap["parent"].(string)

		name := urnName(urn)
		parentName := urnName(parent)
		isRoot := strings.Contains(parent, "pulumi:pulumi:Stack") || parent == ""

		stateInfos[name] = stateResourceInfo{
			name:         name,
			resourceType: resType,
			parentName:   parentName,
			isRoot:       isRoot,
		}
	}

	// Build digest name map.
	nameMap := BuildDigestNameMap(digest, moduleMappings, resourceMappings, nil, stateInfos)

	result := &PatchStateResult{DigestMapped: len(nameMap)}

	// Patch resources.
	for i, raw := range resourcesRaw {
		rMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		custom, _ := rMap["custom"].(bool)
		if !custom {
			continue
		}
		urn, _ := rMap["urn"].(string)
		resType, _ := rMap["type"].(string)
		name := urnName(urn)

		st := shortPulumiType(resType)
		notReadFields, hasFields := notReadByType[st]
		if !hasFields {
			result.NoFields++
			continue
		}

		digResource := nameMap[name]

		inputsRaw, _ := rMap["inputs"].(map[string]interface{})
		outputsRaw, _ := rMap["outputs"].(map[string]interface{})
		if inputsRaw == nil {
			inputsRaw = map[string]interface{}{}
		}
		if outputsRaw == nil {
			outputsRaw = map[string]interface{}{}
		}

		// Build field descriptors from the fields file.
		var fields []patchFieldDescriptor
		for pulumiField, meta := range notReadFields {
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

		patchResult, inputsRaw, outputsRaw, err := patchAndValidateResource(
			urn, name, fields, inputsRaw, outputsRaw, digResource, configSecrets, configDir,
		)
		if err != nil {
			return nil, nil, err
		}
		result.FieldsFromDigest += patchResult.fieldsFromDigest
		result.FieldsFromDefaults += patchResult.fieldsFromDefaults
		result.SkippedSensitive += patchResult.skippedSensitive
		if patchResult.patched {
			result.Patched++
		}
		if patchResult.deltaValidated {
			result.DeltaValidated++
		}
		if patchResult.deltaFailed {
			result.DeltaFailed++
		}
		if !patchResult.patched && digResource == nil {
			result.NoMatch++
		}

		rMap["inputs"] = inputsRaw
		rMap["outputs"] = outputsRaw
		resourcesRaw[i] = rMap
	}

	deployment["resources"] = resourcesRaw
	state["deployment"] = deployment

	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling patched state: %w", err)
	}

	return out, result, nil
}

const (
	assetSig   = "c44067f5952c0a294b673a41bacd8c17"
	archiveSig = "0def7320c3a5731c473e5ecbe6d01bc7"
	sigKey     = "4dabf18193072939515e22adb298388d"
)

// buildAssetSentinel constructs a Pulumi asset/archive sentinel from a file path.
//
// For FileAsset: reads the file and computes SHA256 hash.
//
// For FileArchive: tries these in order:
//  1. Directory (strip .zip) → path-only sentinel, engine computes hash
//  2. Zip file → extracts contents into an AssetArchive sentinel with
//     StringAsset text entries for each file. The engine hashes both the
//     state-side and program-side archives identically.
func buildAssetSentinel(absPath, assetType string) (map[string]interface{}, error) {
	if assetType == "FileArchive" {
		// Try directory first (strip .zip).
		dirPath := strings.TrimSuffix(absPath, ".zip")
		if info, err := os.Stat(dirPath); err == nil && info.IsDir() {
			hash, err := hashFileArchive(dirPath)
			if err != nil {
				return nil, fmt.Errorf("hashing directory %s: %w", dirPath, err)
			}
			return map[string]interface{}{
				sigKey: archiveSig,
				"hash": hash,
				"path": dirPath,
			}, nil
		}

		// Try as a zip file → extract into AssetArchive sentinel.
		if strings.HasSuffix(absPath, ".zip") {
			if _, err := os.Stat(absPath); err == nil {
				return buildAssetArchiveFromZip(absPath)
			}
		}

		// Plain directory path.
		if info, err := os.Stat(absPath); err == nil && info.IsDir() {
			hash, err := hashFileArchive(absPath)
			if err != nil {
				return nil, fmt.Errorf("hashing directory %s: %w", absPath, err)
			}
			return map[string]interface{}{
				sigKey: archiveSig,
				"hash": hash,
				"path": absPath,
			}, nil
		}

		return nil, fmt.Errorf("archive path not found: %s", absPath)
	}

	// FileAsset: hash the file contents.
	f, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", absPath, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, fmt.Errorf("hashing %s: %w", absPath, err)
	}
	hash := hex.EncodeToString(h.Sum(nil))

	return map[string]interface{}{
		sigKey:  assetSig,
		"hash":  hash,
		"path":  absPath,
	}, nil
}

// buildAssetArchiveFromZip reads a zip file and constructs an AssetArchive
// sentinel with StringAsset text entries for each file in the zip.
// This matches how Pulumi's AssetArchive({"file": StringAsset(content)})
// is serialized, allowing the engine to hash both sides identically.
func buildAssetArchiveFromZip(zipPath string) (map[string]interface{}, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("opening zip %s: %w", zipPath, err)
	}
	defer r.Close()

	assets := make(map[string]interface{})
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("reading %s from zip: %w", f.Name, err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("reading %s from zip: %w", f.Name, err)
		}

		// Compute hash of the individual file.
		h := sha256.New()
		h.Write(content)
		hash := hex.EncodeToString(h.Sum(nil))

		assets[f.Name] = map[string]interface{}{
			sigKey: assetSig,
			"hash": hash,
			"text": string(content),
		}
	}

	// Compute overall archive hash using the Pulumi SDK, matching what the
	// engine computes for AssetArchive({"file": StringAsset(content)}).
	archiveAssets := make(map[string]interface{})
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			continue
		}
		a, err := pulumiasset.FromText(string(content))
		if err != nil {
			continue
		}
		archiveAssets[f.Name] = a
	}
	arch, err := pulumiarchive.FromAssets(archiveAssets)
	if err != nil {
		return nil, fmt.Errorf("creating archive for hash: %w", err)
	}
	if err := arch.EnsureHash(); err != nil {
		return nil, fmt.Errorf("computing archive hash: %w", err)
	}

	return map[string]interface{}{
		sigKey:   archiveSig,
		"hash":   arch.Hash,
		"assets": assets,
	}, nil
}

// downloadLambdaCodeAsArchive downloads a Lambda function's deployed code from AWS,
// extracts the zip, and constructs an AssetArchive sentinel with StringAsset entries.
// The region is extracted from the function ARN (arn:aws:lambda:REGION:...).
func downloadLambdaCodeAsArchive(functionName, arn string) (map[string]interface{}, error) {
	ctx := context.Background()

	// Extract region from ARN if available.
	var optFns []func(*awsconfig.LoadOptions) error
	if arn != "" {
		parts := strings.Split(arn, ":")
		if len(parts) >= 4 && parts[3] != "" {
			optFns = append(optFns, awsconfig.WithRegion(parts[3]))
		}
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	client := lambda.NewFromConfig(cfg)
	result, err := client.GetFunction(ctx, &lambda.GetFunctionInput{
		FunctionName: &functionName,
	})
	if err != nil {
		return nil, fmt.Errorf("getting function %s: %w", functionName, err)
	}

	if result.Code == nil || result.Code.Location == nil {
		return nil, fmt.Errorf("no code location for function %s", functionName)
	}

	// Download the zip from the presigned URL.
	resp, err := http.Get(*result.Code.Location)
	if err != nil {
		return nil, fmt.Errorf("downloading code for %s: %w", functionName, err)
	}
	defer resp.Body.Close()

	// Write to temp file (zip.OpenReader needs a file).
	tmpFile, err := os.CreateTemp("", "lambda-code-*.zip")
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("writing code zip: %w", err)
	}
	tmpFile.Close()

	return buildAssetArchiveFromZip(tmpFile.Name())
}

// patchedOutputFieldInfo tracks an output field that was modified by the patcher.
type patchedOutputFieldInfo struct {
	pulumiField string
	oldValue    interface{}
	newValue    interface{}
}

// assetFieldDeltaInfo holds info needed to inject an asset delta entry.
type assetFieldDeltaInfo struct {
	pulumiField string
	tfField     string
	kind        int
	format      int
	hashField   string
}

// injectAssetDeltas updates the __pulumi_raw_state_delta to include asset delta
// entries for patched asset fields. The delta is a nested obj structure; we add
// property delta entries matching the bridge's assetDelta format.
func injectAssetDeltas(deltaRaw interface{}, fields []assetFieldDeltaInfo) interface{} {
	delta, ok := deltaRaw.(map[string]interface{})
	if !ok || delta == nil {
		return deltaRaw
	}

	// The top-level delta should be an "obj" with "ps" (property deltas).
	obj, ok := delta["obj"].(map[string]interface{})
	if !ok {
		return deltaRaw
	}
	ps, ok := obj["ps"].(map[string]interface{})
	if !ok {
		ps = map[string]interface{}{}
		obj["ps"] = ps
	}

	for _, f := range fields {
		// Build the asset delta entry matching the bridge's assetDelta JSON format.
		assetDelta := map[string]interface{}{
			"kind": f.kind,
		}
		if f.format != 0 {
			assetDelta["archiveFormat"] = f.format
		}
		if f.hashField != "" {
			assetDelta["hashField"] = f.hashField
		}
		ps[f.pulumiField] = map[string]interface{}{
			"asset": assetDelta,
		}
	}

	return delta
}

// updateDeltaForPatchedOutputs updates the __pulumi_raw_state_delta when the patcher
// changes output fields that are referenced by the delta. When an array goes from
// empty to populated (or changes element count), the delta's element entries must
// be updated to match, otherwise the bridge's Recover panics with
// "rawStateRecoverNatural cannot process Object values due to map vs object confusion".
func updateDeltaForPatchedOutputs(deltaRaw interface{}, fields []patchedOutputFieldInfo) interface{} {
	delta, ok := deltaRaw.(map[string]interface{})
	if !ok || delta == nil {
		return deltaRaw
	}

	obj, ok := delta["obj"].(map[string]interface{})
	if !ok {
		return deltaRaw
	}
	ps, ok := obj["ps"].(map[string]interface{})
	if !ok {
		return deltaRaw
	}

	for _, f := range fields {
		existing, inDelta := ps[f.pulumiField]
		if !inDelta {
			continue // field not in delta, no update needed
		}

		// Check if the existing delta entry is an array type.
		existingMap, ok := existing.(map[string]interface{})
		if !ok {
			continue
		}
		arrDelta, isArr := existingMap["arr"]
		if !isArr {
			continue
		}

		// Rebuild the array delta to match the new value's structure.
		newArr, ok := f.newValue.([]interface{})
		if !ok {
			continue
		}

		newArrDelta := buildArrayDelta(arrDelta, newArr)
		ps[f.pulumiField] = map[string]interface{}{
			"arr": newArrDelta,
		}
	}

	return delta
}

// buildArrayDelta builds an array delta entry that matches the given array value.
// It preserves existing element deltas where possible and adds new entries for
// elements that are objects (which need explicit "obj" deltas for Recover to work).
func buildArrayDelta(existingArrDelta interface{}, newArr []interface{}) map[string]interface{} {
	result := map[string]interface{}{}

	// Copy existing array delta fields (except "el" which we'll rebuild).
	if existing, ok := existingArrDelta.(map[string]interface{}); ok {
		for k, v := range existing {
			if k != "el" {
				result[k] = v
			}
		}
	}

	// Build element deltas for each element in the new array.
	// Objects need explicit {"obj": {}} entries; primitives don't need entries.
	elements := map[string]interface{}{}
	for i, elem := range newArr {
		delta := buildValueDelta(elem)
		if delta != nil {
			elements[fmt.Sprintf("%d", i)] = delta
		}
	}

	if len(elements) > 0 {
		result["el"] = elements
	}

	return result
}

// buildValueDelta builds a delta entry for a value based on its type.
// Returns nil for primitive types (string, bool, number) which don't need
// explicit delta entries — rawStateRecoverNatural handles them.
// Returns {"obj": {}} for objects and {"arr": ...} for nested arrays.
func buildValueDelta(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		// Check if this is a Pulumi sentinel (asset/archive/secret) — skip those,
		// they're handled by the asset delta injection or naturally.
		if _, isSentinel := val[sigKey]; isSentinel {
			return nil
		}
		return map[string]interface{}{"obj": map[string]interface{}{}}
	case []interface{}:
		return map[string]interface{}{"arr": buildArrayDelta(nil, val)}
	default:
		return nil // primitives don't need delta entries
	}
}

// hashFileArchive computes the hash of a directory archive using the Pulumi SDK's
// archive package, which is the exact same hashing the engine uses for FileArchive.
// This ensures the hash in our sentinel matches the program-side hash.
func hashFileArchive(dirPath string) (string, error) {
	arch, err := pulumiarchive.FromPath(dirPath)
	if err != nil {
		return "", fmt.Errorf("creating archive from %s: %w", dirPath, err)
	}
	if err := arch.EnsureHash(); err != nil {
		return "", fmt.Errorf("computing hash for %s: %w", dirPath, err)
	}
	return arch.Hash, nil
}

// isEmptyValue checks if a value is nil, empty string, or empty array/map.
// equalValues compares two JSON-deserialized values for equality.
// JSON numbers from different sources may be float64 vs int, so we
// normalize numeric comparisons.
func equalValues(a, b interface{}) bool {
	return reflect.DeepEqual(a, b)
}

// conformToDelta transforms a digest value to match the bridge's type mapping
// by reading the __pulumi_raw_state_delta for the field. When the bridge has
// flattened a TF TypeSet (array) into a Pulumi object, the delta records "obj"
// for that field. If we're patching the output with a digest value that's an
// array (TF representation), we need to flatten it to match the bridge's
// object representation so Recover can correctly reverse the transformation.
func conformToDelta(val interface{}, field string, outputsRaw map[string]interface{}) interface{} {
	deltaRaw, ok := outputsRaw["__pulumi_raw_state_delta"]
	if !ok {
		return val
	}
	delta, ok := deltaRaw.(map[string]interface{})
	if !ok {
		return val
	}
	obj, ok := delta["obj"].(map[string]interface{})
	if !ok {
		return val
	}
	ps, ok := obj["ps"].(map[string]interface{})
	if !ok {
		return val
	}
	fieldDelta, ok := ps[field].(map[string]interface{})
	if !ok {
		return val
	}

	// Check for "plu" (Pulumi-specific type mapping).
	// The bridge transforms TF types — e.g. flattening TypeSets to objects.
	if plu, ok := fieldDelta["plu"].(map[string]interface{}); ok {
		arr, isArr := val.([]interface{})
		if isArr && len(arr) == 0 {
			// Empty array from digest + Pulumi type mapping = keep nil.
			// Setting an empty array breaks the delta contract.
			return nil
		}
		if inner, ok := plu["i"].(map[string]interface{}); ok {
			if _, isObj := inner["obj"]; isObj {
				// Delta says object, but digest value is array — flatten it.
				if isArr && len(arr) > 0 {
					if elem, isMap := arr[0].(map[string]interface{}); isMap {
						result := camelCaseKeys(elem)
						// Recursively conform nested fields that also have plu deltas.
						if nestedPs, ok := inner["obj"].(map[string]interface{}); ok {
							if ps, ok := nestedPs["ps"].(map[string]interface{}); ok {
								if resultMap, ok := result.(map[string]interface{}); ok {
									for nestedField, nestedDelta := range ps {
										if nestedVal, exists := resultMap[nestedField]; exists {
											// Build a synthetic outputsRaw with just the delta for recursion.
											synth := map[string]interface{}{
												"__pulumi_raw_state_delta": map[string]interface{}{
													"obj": map[string]interface{}{
														"ps": map[string]interface{}{
															nestedField: nestedDelta,
														},
													},
												},
											}
											resultMap[nestedField] = conformToDelta(nestedVal, nestedField, synth)
										}
									}
								}
							}
						}
						return result
					}
				}
			}
		}
	}

	return val
}

// isSimpleValue returns true for primitive types (bool, number, string, nil).
// Arrays and objects are not simple — they may have been type-mapped by the bridge.
func isSimpleValue(v interface{}) bool {
	switch v.(type) {
	case nil, bool, string, float64, int:
		return true
	}
	return false
}

// propertyValueFromState converts a JSON-deserialized state value into a
// resource.PropertyValue, recognizing Pulumi sentinel maps (assets, archives,
// secrets) and converting them to properly typed PropertyValues.
//
// resource.NewPropertyValue treats sentinel maps as plain objects, which causes
// pv.IsAsset() etc. to return false. This function uses the SDK's
// DeserializeAsset/DeserializeArchive to produce correct types, matching how
// the engine deserializes state for the bridge's Diff/Recover path.
func propertyValueFromState(v interface{}) resource.PropertyValue {
	replv := func(v interface{}) (resource.PropertyValue, bool) {
		m, ok := v.(map[string]interface{})
		if !ok {
			return resource.PropertyValue{}, false
		}
		s, hasSig := m[sigKey].(string)
		if !hasSig {
			return resource.PropertyValue{}, false
		}
		switch s {
		case "1b47061264138c4ac30d75fd1eb44270": // secret
			elem := propertyValueFromState(m["value"])
			return resource.MakeSecret(elem), true
		default:
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

// validateRecover checks that a resource's outputs are compatible with its
// __pulumi_raw_state_delta by running the bridge's Recover function.
// Returns nil if valid, or the Recover error if not.
func validateRecover(urn string, outputsRaw map[string]interface{}) error {
	deltaRaw, ok := outputsRaw["__pulumi_raw_state_delta"]
	if !ok {
		return nil
	}
	deltaMap, ok := deltaRaw.(map[string]interface{})
	if !ok {
		return nil
	}

	outputsPV := propertyValueFromState(outputsRaw)
	deltaPV := resource.NewPropertyValue(deltaMap)
	rsd, err := tfbridge.UnmarshalRawStateDelta(deltaPV)
	if err != nil {
		return fmt.Errorf("UnmarshalRawStateDelta: %w", err)
	}
	if _, err := rsd.Recover(outputsPV); err != nil {
		return fmt.Errorf("Recover: %w", err)
	}
	return nil
}

func isEmptyValue(v interface{}) bool {
	if v == nil || v == "" {
		return true
	}
	switch val := v.(type) {
	case []interface{}:
		return len(val) == 0
	case map[string]interface{}:
		return len(val) == 0
	}
	return false
}

// snakeToCamel converts a snake_case string to camelCase.
func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// camelCaseKeys recursively converts snake_case keys to camelCase in maps and
// arrays. Used when copying digest values (TF snake_case) to Pulumi state
// (camelCase).
func camelCaseKeys(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{}, len(val))
		for k, v := range val {
			result[snakeToCamel(k)] = camelCaseKeys(v)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, v := range val {
			result[i] = camelCaseKeys(v)
		}
		return result
	default:
		return v
	}
}

// isAssetOrArchiveSentinel checks if a value is a Pulumi asset or archive sentinel.
func isAssetOrArchiveSentinel(v interface{}) bool {
	m, ok := v.(map[string]interface{})
	if !ok {
		return false
	}
	sig, _ := m[sigKey].(string)
	return sig == assetSig || sig == archiveSig
}

// isSecretSentinel checks if a value is a Pulumi secret sentinel map.
func isSecretSentinel(v interface{}) bool {
	m, ok := v.(map[string]interface{})
	if !ok {
		return false
	}
	_, hasSig := m["4dabf18193072939515e22adb298388d"]
	return hasSig
}

// unwrapSecretSentinel extracts the raw value from a secret sentinel.
// If the value is not a sentinel, it is returned unchanged.
// The "plaintext" field contains a JSON-encoded value (e.g. "\"foo\"" for string "foo").
func unwrapSecretSentinel(v interface{}) interface{} {
	m, ok := v.(map[string]interface{})
	if !ok {
		return v
	}
	if _, hasSig := m["4dabf18193072939515e22adb298388d"]; !hasSig {
		return v
	}
	plaintext, ok := m["plaintext"].(string)
	if !ok {
		return v
	}
	var raw interface{}
	if err := json.Unmarshal([]byte(plaintext), &raw); err != nil {
		return v
	}
	return raw
}

// isNullSentinel checks if a value is a secret sentinel wrapping null/empty.
// This happens when cloud import creates a sentinel for a write-only field
// where the provider Read returns nil.
func isNullSentinel(v interface{}) bool {
	m, ok := v.(map[string]interface{})
	if !ok {
		return false
	}
	if _, hasSig := m["4dabf18193072939515e22adb298388d"]; !hasSig {
		return false
	}
	// Check plaintext or ciphertext for null/empty values.
	if pt, ok := m["plaintext"]; ok {
		s, isStr := pt.(string)
		return !isStr || s == "" || s == "null" || s == `""`
	}
	return false
}

// normalizeTFName extracts the for_each key from a TF resource name.
// resourceName["key"] → key (strips any resource name prefix and brackets/quotes)
// resourceName[0] → 0
// plain_name → plain_name (no for_each key)
func normalizeTFName(name string) string {
	idx := strings.Index(name, "[")
	if idx < 0 {
		return name
	}
	key := name[idx+1 : len(name)-1] // strip [ and ]
	key = strings.Trim(key, `"`)
	return key
}

// urnName extracts the last segment of a URN.
func urnName(urn string) string {
	parts := strings.Split(urn, "::")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// PatchStateFromSchema patches imported state using provider schema instead of a hand-curated
// fields file. For each resource, it looks up the provider schema and iterates ALL
// input fields, patching any that are nil/empty in the imported state from the digest
// or schema defaults.
//
// configSecrets is an optional map of config key -> decrypted value, used to resolve
// sensitive fields that the digest redacts as "(sensitive)".
// configDir is the TF config directory, used to resolve asset file paths.
func PatchStateFromSchema(
	stateData []byte,
	digest *ModuleMap,
	providers map[providermap.TerraformProviderName]*ProviderWithMetadata,
	pulumiToTFType map[string]string,
	moduleMappings, resourceMappings map[string]string,
	configSecrets map[string]string,
	configDir string,
) ([]byte, *PatchStateResult, error) {
	// Parse state using a decoder with UseNumber to preserve exact number
	// representations. Without this, large integers (like AWS account IDs)
	// become float64 and may re-serialize as scientific notation (e.g.,
	// "5399223e-54"), which Pulumi's state parser rejects.
	var state map[string]interface{}
	dec := json.NewDecoder(strings.NewReader(string(stateData)))
	dec.UseNumber()
	if err := dec.Decode(&state); err != nil {
		return nil, nil, fmt.Errorf("parsing state: %w", err)
	}

	deployment, ok := state["deployment"].(map[string]interface{})
	if !ok {
		return nil, nil, fmt.Errorf("state missing deployment")
	}

	resourcesRaw, ok := deployment["resources"].([]interface{})
	if !ok {
		return nil, nil, fmt.Errorf("state missing resources")
	}

	// Extract resource info from state for matching.
	stateInfos := make(map[string]stateResourceInfo)
	for _, raw := range resourcesRaw {
		rMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		custom, _ := rMap["custom"].(bool)
		if !custom {
			continue
		}
		urn, _ := rMap["urn"].(string)
		resType, _ := rMap["type"].(string)
		parent, _ := rMap["parent"].(string)

		name := urnName(urn)
		parentName := urnName(parent)
		isRoot := strings.Contains(parent, "pulumi:pulumi:Stack") || parent == ""

		stateInfos[name] = stateResourceInfo{
			name:         name,
			resourceType: resType,
			parentName:   parentName,
			isRoot:       isRoot,
		}
	}

	// Build digest name map.
	nameMap := BuildDigestNameMap(digest, moduleMappings, resourceMappings, nil, stateInfos)

	result := &PatchStateResult{DigestMapped: len(nameMap)}

	// Patch resources.
	for i, raw := range resourcesRaw {
		rMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		custom, _ := rMap["custom"].(bool)
		if !custom {
			continue
		}
		urn, _ := rMap["urn"].(string)
		resType, _ := rMap["type"].(string)
		name := urnName(urn)

		// Look up provider schema for this resource type.
		prov, tfType, provOK := LookupProviderForPulumiType(resType, pulumiToTFType, providers)
		if !provOK {
			result.NoFields++
			continue
		}
		schemaFields := GetSchemaFieldInfo(prov, tfType)
		if len(schemaFields) == 0 {
			result.NoFields++
			continue
		}

		digResource := nameMap[name]

		inputsRaw, _ := rMap["inputs"].(map[string]interface{})
		outputsRaw, _ := rMap["outputs"].(map[string]interface{})
		if inputsRaw == nil {
			inputsRaw = map[string]interface{}{}
		}
		if outputsRaw == nil {
			outputsRaw = map[string]interface{}{}
		}

		// Build field descriptors from the schema.
		var fields []patchFieldDescriptor
		for _, fieldInfo := range schemaFields {
			if fieldInfo.IsComputed && !fieldInfo.IsInput {
				continue
			}
			fd := patchFieldDescriptor{
				PulumiName: fieldInfo.PulumiName,
				TFName:     fieldInfo.TFName,
				HasDefault: fieldInfo.HasDefault,
				Default:    fieldInfo.SchemaDefault,
				HashField:  fieldInfo.HashField,
			}
			if fieldInfo.IsAsset {
				kind := fieldInfo.AssetKind
				fd.AssetKind = &kind
				format := int(fieldInfo.ArchiveFormat)
				fd.ArchiveFormat = &format
				switch info.AssetTranslationKind(fieldInfo.AssetKind) {
				case info.FileAsset:
					fd.AssetType = "FileAsset"
				case info.FileArchive:
					fd.AssetType = "FileArchive"
				}
			}
			fields = append(fields, fd)
		}

		patchResult, inputsRaw, outputsRaw, err := patchAndValidateResource(
			urn, name, fields, inputsRaw, outputsRaw, digResource, configSecrets, configDir,
		)
		if err != nil {
			return nil, nil, err
		}
		result.FieldsFromDigest += patchResult.fieldsFromDigest
		result.FieldsFromDefaults += patchResult.fieldsFromDefaults
		result.SkippedSensitive += patchResult.skippedSensitive
		if patchResult.patched {
			result.Patched++
		}
		if patchResult.deltaValidated {
			result.DeltaValidated++
		}
		if patchResult.deltaFailed {
			result.DeltaFailed++
		}
		if !patchResult.patched && digResource == nil {
			result.NoMatch++
		}

		rMap["inputs"] = inputsRaw
		rMap["outputs"] = outputsRaw
		resourcesRaw[i] = rMap
	}

	deployment["resources"] = resourcesRaw
	state["deployment"] = deployment

	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling patched state: %w", err)
	}

	return out, result, nil
}

// LoadFieldsFile reads and parses an aws-import-diff-fields.json file.
func LoadFieldsFile(path string) (*FieldsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading fields file: %w", err)
	}
	var ff FieldsFile
	if err := json.Unmarshal(data, &ff); err != nil {
		return nil, fmt.Errorf("parsing fields file: %w", err)
	}
	return &ff, nil
}
