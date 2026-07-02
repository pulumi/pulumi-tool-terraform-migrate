# Falsy Default Suppression in patch-state

## Problem

The Pulumi Terraform bridge v3.127.0+ suppresses falsy TF schema defaults (`false`, `0`, `""`) during Check via `shouldSuppressTFSchemaDefaultValue` (PR #3398). This means when a program doesn't set a field like `forceDestroy`, the bridge sends `null` to the TF diff engine rather than the schema default `false`.

When `patch-state` patches these falsy defaults into the imported state (e.g., `forceDestroy: false`), the preview shows a phantom diff of `false → null` because:
- Old inputs (from patched state): `false`
- New inputs (from bridge Check): `null` (falsy default suppressed)

If patch-state does NOT patch these fields, both old and new are `null` → no diff.

The suppression behavior is tied to the bridge version, which is embedded in the provider binary. For the AWS provider specifically, v7.27.0 is the first version using bridge v3.127.0.

## Solution

Add provider-version-aware logic to patch-state that skips patching falsy defaults when the provider version is known to suppress them.

### Fields file changes

Add a top-level `falsyDefaultSuppression` map to `aws-import-diff-fields.json`:

```json
{
  "falsyDefaultSuppression": {
    "aws": "7.27.0"
  },
  "fields": { ... }
}
```

Keys are provider package names (extracted from the Pulumi type token, e.g., `aws:s3/bucket:Bucket` → `aws`). Values are the minimum provider version at which the bridge suppresses falsy defaults. Any bridged provider can be added here as the behavior is discovered.

### Code changes in `state_patcher.go`

#### 1. Parse `falsyDefaultSuppression` from fields file

Add `FalsyDefaultSuppression map[string]string` to the `FieldsFile` struct, tagged `json:"falsyDefaultSuppression,omitempty"`.

#### 2. Build provider version map

In `PatchState`, before patching resources, iterate `resourcesRaw` to collect provider resources (`type` starts with `pulumi:providers:`). Extract each provider's URN and version from `inputs.version`. Build a `map[string]string` of provider URN → version.

#### 3. Helper: extract provider package from resource type

```go
func providerPackage(resourceType string) string
```

`aws:s3/bucket:Bucket` → `aws`. Split on `:`, return first element.

#### 4. Helper: resolve provider version for a resource

Each resource has a `provider` field containing a URN with a trailing UUID:
```
urn:pulumi:stack::project::pulumi:providers:aws::name::UUID
```

Strip the UUID (everything after the last `::`) to get the provider URN, then look up the version in the map.

```go
func resolveProviderVersion(providerRef string, providerVersions map[string]string) string
```

#### 5. Helper: check if a default is falsy

Match the bridge's `shouldSuppressTFSchemaDefaultValue` exactly:

```go
func isFalsyDefault(v interface{}) bool
```

- `bool`: falsy if `false`
- `float64`: falsy if `0`
- `json.Number`: falsy if `0`
- `string`: falsy if `""`
- everything else: not falsy

#### 6. Helper: semver comparison

```go
func semverAtLeast(version, minimum string) bool
```

Split on `.`, parse each segment as int, compare left to right. Malformed versions return `false` (don't suppress — safer to patch than to skip).

#### 7. Skip falsy defaults in the patching loop

In the `PatchState` function, when building field descriptors for a resource:

1. Extract provider package from resource type
2. Check if `falsyDefaultSuppression` has an entry for that package
3. Resolve the resource's provider version from state
4. If version >= threshold, exclude fields where `isFalsyDefault(field.Default)` is true

The exclusion happens at the field-descriptor-building stage (lines 664-677), before `patchAndValidateResource` is called. Fields with non-falsy defaults or no default (asset/content fields with `null` default) are unaffected.

#### 8. Stats

Add `SkippedFalsySuppressed int` to `PatchStateResult`. Print it in `cmd/patch_state.go` output:
```
Skipped falsy suppressed: N
```

### What is NOT changed

- The fields file field definitions remain the same — falsy defaults are still listed with their `"default"` values for documentation and for use with older provider versions.
- `PatchStateFromSchema` (the removed schema-driven path) is not affected.
- No CLI flags are added — the behavior is driven entirely by the fields file metadata and provider versions in state.

### Falsy default examples

| Field | Default | Falsy? | Patched with AWS >= 7.27.0? |
|-------|---------|--------|-----------------------------|
| `forceDestroy` | `false` | yes | no |
| `applyImmediately` | `false` | yes | no |
| `recoveryWindowInDays` | `30` | no | yes |
| `confirmationTimeoutInMinutes` | `1` | no | yes |
| `content` | `null` | n/a (no default) | yes (from digest) |
| `secretString` | `null` | n/a (no default) | yes (from digest) |

### Testing

- Unit test: `isFalsyDefault` with bool, number, string, nil, map cases
- Unit test: `semverAtLeast` with equal, greater, less, malformed cases
- Unit test: `providerPackage` with standard type tokens
- Integration test: patch a state with AWS provider v7.34.0, fields file with `falsyDefaultSuppression.aws = "7.27.0"`, verify that `forceDestroy: false` is NOT patched but `recoveryWindowInDays: 30` IS patched
- Integration test: same state but provider version v7.26.0, verify `forceDestroy: false` IS patched
