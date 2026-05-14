# pulumi-tool-tf-digest

**Date:** 2026-05-14
**Repository:** `github.com/pulumi-proserv/pulumi-tool-tf-digest` (new repo, no fork)
**Distribution:** `pulumi plugin install tool tf-digest`

## Purpose

Parse Terraform configuration and state into an enriched, agent-safe metadata file (the "digest"). The digest contains everything an agent needs to write Pulumi components and programs — resource types, import IDs, non-sensitive attributes, module interfaces — with sensitive values redacted using provider schema markings. A second command extracts secret values from state and sets them as encrypted Pulumi stack config, so the agent never sees plaintext secrets.

## Commands

### `generate`

Produces the digest file from TF config + raw `.tfstate`.

```
pulumi tf-digest generate \
  --from <tf-config-dir> \
  --state-file <raw-tfstate-path> \
  --out <digest.json> \
  --pulumi-stack <stack-name> \
  --pulumi-project <project-name>
```

All flags required.

**What it does:**
1. Loads TF config from `--from` directory
2. Auto-runs `tofu init -backend=false` if modules aren't installed (tolerates version constraint errors if modules.json is created)
3. Loads raw `.tfstate` from `--state-file`
4. Downloads provider binaries via the registry, queries their schemas via gRPC to get `Sensitive` attribute markings
5. Builds the digest: modules with interfaces, root resources, all with import IDs and attributes (sensitive values replaced with `"(sensitive)"`)
6. Optionally creates a tofu evaluation context for expression evaluation (graceful degradation if it fails)
7. Resolves Pulumi provider mappings for URN translation
8. Writes the digest to `--out`

**Only raw `.tfstate` format is supported.** No `tofu show -json` format.

### `set-secrets`

Extracts secret values from raw `.tfstate` and sets them as encrypted secrets in Pulumi stack config. The agent constructs the mappings but never sees the values.

```
pulumi tf-digest set-secrets \
  --state-file <raw-tfstate-path> \
  --project-dir <pulumi-project-dir> \
  --stack <stack-name> \
  --map 'configKey=terraformAddress:attribute' \
  --map 'configKey2=terraformAddress2:attribute2'
```

`--state-file` and `--stack` are required. `--project-dir` defaults to `.`.

**What it does:**
1. Reads and parses the raw `.tfstate`
2. Builds a lookup map of terraform addresses to attributes
3. Initializes the Pulumi stack if it doesn't exist
4. For each `--map`, finds the resource by address, extracts the attribute value, runs `pulumi config set --secret`

**Map format:** `configKey=terraformAddress:attribute`
- `configKey` — the Pulumi config key to set
- `terraformAddress` — full terraform resource address (supports module paths and for_each index keys)
- `attribute` — the attribute name on the resource (separated from address by last `:`)

## Digest File Format

```json
{
  "modules": {
    "moduleName[\"indexKey\"]": {
      "terraformPath": "module.moduleName",
      "source": "git@github.com:org/tfmod.git//Module/?ref=v1.0.0",
      "indexKey": "indexKey",
      "indexType": "string",
      "resources": [
        {
          "mode": "managed",
          "translatedUrn": "urn:pulumi:stack::project::provider:type::name",
          "terraformAddress": "module.moduleName.aws_resource.name",
          "importId": "resource-id",
          "attributes": { "name": "...", "arn": "...", "secret_field": "(sensitive)" }
        }
      ],
      "interface": {
        "inputs": [{ "name": "varName", "type": "string", "required": true }],
        "outputs": [{ "name": "outputName" }]
      }
    }
  },
  "rootResources": [
    {
      "mode": "managed",
      "translatedUrn": "...",
      "terraformAddress": "aws_s3_bucket.example",
      "importId": "my-bucket",
      "attributes": { "bucket": "my-bucket", "arn": "...", "tags": {} }
    },
    {
      "mode": "data",
      "terraformAddress": "data.aws_caller_identity.this",
      "importId": "123456789",
      "attributes": { "account_id": "123456789" }
    }
  ]
}
```

**Attribute handling:**
- Data sources: all attributes included (no secrets in data sources)
- Managed resources: all attributes included, with sensitive values (per provider schema `Sensitive: true`) replaced by `"(sensitive)"`

## Code to Port

From `pulumi-proserv/pulumi-tool-terraform-migrate`:

| File | Purpose | Changes |
|------|---------|---------|
| `pkg/module_map.go` | BuildModuleMap, matchResources, ModuleMap types | Rename to digest types |
| `pkg/generate_module_map.go` | GenerateModuleMap orchestrator | Remove tofuShowJSON path, DetectStateFormat, rawStateFromTfjson |
| `pkg/tofu_eval.go` | LoadConfig, LoadRawState, Evaluate, runTofuInit | Keep as-is |
| `pkg/provider_schema.go` | BuildSensitivityMap, RedactSensitiveAttributes | Keep as-is |
| `pkg/set_secrets.go` | SetSecrets, ParseSecretMapping | Keep as-is |
| `pkg/pulumi_providers.go` | PulumiProvidersForTerraformProviders | Keep as-is |
| `pkg/providermap/` | TF→Pulumi provider/URN translation | Keep as-is |
| `pkg/tfprovider/` | Provider download/cache/launch via gRPC | Keep as-is |
| `cmd/root.go` | Root cobra command | Update name and description |
| `cmd/module_map.go` | module-map command | Rename to `generate` |
| `cmd/set_secrets.go` | set-secrets command | Keep as-is |

## Code to Drop

| File/Package | Reason |
|---|---|
| `cmd/stack.go` | Stack state migration — not needed |
| `cmd/show_state.go` | Debug utility — not needed |
| `cmd/update_providermap.go` | Admin utility — not needed at runtime |
| `pkg/state_adapter.go` | Stack command state conversion |
| `pkg/pulumi_state.go` | Pulumi state export for stack command |
| `pkg/convert_tf_value_to_pulumi.go` | Value conversion for stack command |
| `pkg/module_tree.go` | Component tree building — unused by generate |
| `pkg/tofu/` | tofu show -json loading — raw state only |
| `pkg/bridge/` | If only used by state adapter |
| All `tfjson` / `StateFormatTofuShowJSON` code | Simplified to raw state only |

## Skill Updates (same PR to pulumi-migration-assets)

### pulumi-terraform-workspace-migration
- Update Prerequisites: remove `pulumi-linter`, add `pulumi plugin install tool tf-digest`
- Update Phase 1a: `pulumi tf-digest generate` instead of `pulumi-terraform-migrate module-map`
- Update Phase 5b: `pulumi tf-digest set-secrets` instead of full path to binary
- Remove all references to building from source / Go build commands

### pulumi-terraform-module-to-component
- Remove `pulumi-linter` from Prerequisites and Validation section
- Remove linter violation troubleshooting entries (PUL001, PUL003, PUL005, PUL007, PUL008)

## Publishing as a Pulumi Plugin

For `pulumi plugin install tool tf-digest` to work, the binary must be published as a GitHub release with the naming convention:

```
pulumi-tool-tf-digest-v<version>-<os>-<arch>.tar.gz
```

The release workflow:
1. Tag the repo with `v<version>`
2. CI builds binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64
3. Creates a GitHub release with the tagged version
4. Uploads the tarballed binaries

Users install with:
```
pulumi plugin install tool tf-digest --server github://api.github.com/pulumi-proserv/pulumi-tool-tf-digest
```

## Testing

- Port existing unit tests for module_map, provider_schema, set_secrets
- Drop tests for removed code (state_adapter, convert_tf_value, module_tree, tofu/)
- CI: build + test on push, release on tag
