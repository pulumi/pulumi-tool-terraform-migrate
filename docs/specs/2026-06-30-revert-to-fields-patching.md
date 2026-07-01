# Revert to Fields-Based Patching

## Summary

Revert `patch-state` default behavior from schema-driven patching back to fields-based patching using the curated `aws-import-diff-fields.json`. Keep the Recover bridge test infrastructure from the schema-driven work.

## Problem

Schema-driven patching (merged in PR #17) patches too aggressively. It treats every nil schema-valid input as needing a patch, which introduces phantom diffs for fields the program never sets:

- `sourceHash` on `aws:s3/bucketObject:BucketObject` — valid schema input, digest has a value, so schema-driven patches it in. But the program uses `FileAsset` which handles change detection via the asset's internal hash, not `sourceHash`. Result: 12 delete diffs on swagger UI objects.
- `tags` on bare resources — schema-driven patches tags from digest, but the provider applies tags via `defaultTags` on the provider config. The program doesn't set tags explicitly on individual resources. Result: tag delete diffs on every bare resource.

The curated fields list avoids these problems because it only patches fields that are:
1. Not returned by the cloud API on import
2. Actually set by the program (e.g., `forceDestroy`, `acl`)

## Changes

### 1. Make fields-based the default, schema-driven opt-in

In `cmd/patch_state.go`:
- Make `--fields` required (or default to the bundled fields file path)
- Add `--schema-driven` flag to opt into the schema-based path
- Update help text to reflect the change

### 2. Keep Recover bridge test infrastructure

The following should remain unchanged — they're valuable for validating any patching approach:

| Item | Location |
|---|---|
| `validatePatchedOutputsAgainstDelta` | `pkg/state_patcher_bridge_test.go` |
| `propertyValueFromState` | `pkg/state_patcher.go` |
| `buildTestStateIO` and helpers | `pkg/state_patcher_bridge_test.go` |
| Bridge dependency (v3.130.0) | `go.mod` |

Adapt the bridge tests to validate fields-based patching output. The `validatePatchedOutputsAgainstDelta` function works on any patched state — it just checks that output values are compatible with their `__pulumi_raw_state_delta` entries.

### 3. Keep PatchStateFromSchema available

Don't delete `PatchStateFromSchema` — keep it behind the `--schema-driven` flag for future use. It may be useful once we have a way to constrain which fields it patches (e.g., via preview reconciliation).

### 4. Keep asset patching in fields-based path

The fields-based `PatchState` already handles asset sentinel construction (`buildAssetSentinel`, Lambda code download). This is the critical patching that must work for QA migration.

## Branch

Create `fix/revert-to-fields-patching` from `main` (`4c5d03f`).

Do NOT merge `feat/recover-validated-delta-conforming` — that branch adds delta conforming on top of schema-driven, which is moot if schema-driven isn't the default.

## Verification

After the change, run `patch-state` on the QA state with `--fields`:

```sh
pulumi plugin run terraform-migrate -- patch-state \
  --state state.json \
  --digest /tmp/qa-digest.json \
  --fields aws-import-diff-fields.json \
  --mapping-file mappings.yaml \
  --out patched.json
```

Then `pulumi stack import` and `pulumi preview --diff`. Expected:
- No `sourceHash` diffs (not in fields list, not patched)
- No phantom tag diffs on bare resources (not patched)
- `forceDestroy` gets correct default from fields list
- Asset fields (`source` on Lambda) patched correctly

## Non-goals

- Adding new fields to `aws-import-diff-fields.json` (separate task, as needed)
- Preview-driven reconciliation (future work, separate spec)
- Deleting `PatchStateFromSchema` (keep for future use)
