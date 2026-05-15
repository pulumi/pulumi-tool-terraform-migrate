# `--skip-sensitivity` Flag and Sensitivity Map Disk Cache

## Problem

`BuildSensitivityMap` downloads Terraform provider binaries, starts them as subprocesses, and queries their full schemas via gRPC to find sensitive attributes. For the AWS provider this takes 30+ minutes. The sensitivity map is only used for attribute redaction in module-map output — it's optional.

## Design

### `--skip-sensitivity` flag

New boolean flag on the `module-map` command. When set, `BuildSensitivityMap` is skipped entirely — `nil` is passed to `BuildModuleMap`. No provider downloads, no schema loading, no subprocess spawning.

### Disk cache

**Cache location:** `~/.pulumi/sensitivity-cache/{provider-key}@{version}.json`

Provider key is the provider address with `/` replaced by `-` (e.g. `registry.opentofu.org-hashicorp-aws@5.100.0.json`).

**Cache key:** provider address + resolved version. The same provider+version always produces the same schema, so the cache is safe indefinitely — no TTL needed.

**Cache format:** JSON object mapping resource type to sensitive attribute map:

```json
{
  "aws_secretsmanager_secret_version": {"secret_string": true},
  "aws_db_instance": {"password": true}
}
```

**Flow in `BuildSensitivityMap` (per provider):**

1. Resolve provider version (existing step — fast, just a registry query)
2. Build cache file path from provider address + version
3. If cache file exists: read and unmarshal, merge into sensitivity map, **skip LoadProvider + GetProviderSchema entirely**
4. If cache file does not exist: load provider, get schema, walk schema for sensitive attributes (existing logic), **write cache file**, merge into sensitivity map

Cache read/write errors are non-fatal — fall back to the existing uncached path on read failure, log a warning on write failure.

### Changes

| File | Change |
|---|---|
| `cmd/module_map.go` | Add `--skip-sensitivity` bool flag, pass to `GenerateModuleMap` |
| `pkg/generate_module_map.go` | Add `skipSensitivity bool` parameter, skip `BuildSensitivityMap` call when true |
| `pkg/provider_schema.go` | Add `sensitivityCacheDir()`, `readCachedSensitivity()`, `writeCachedSensitivity()` helpers; wrap per-provider loop with cache check |

### CLI

```bash
# Skip sensitivity entirely (fastest — no provider loading at all)
pulumi-terraform-migrate module-map --skip-sensitivity ...

# Normal (uses cache on second run)
pulumi-terraform-migrate module-map ...
```
