# pulumi-tool-terraform-migrate

This is a fork of `pulumi/pulumi-tool-terraform-migrate` that extends the tool with commands to support Pulumi's native `pulumi import` workflow. The upstream tool provides a `stack` command for direct state translation (TF state → Pulumi state). This fork adds commands that supplement the standard import process:

- **`tf-digest`** — Digests TF sources + state into an agent-safe JSON sidecar, auto-discovers secrets, and sets them as Pulumi stack config
- **`import-id-match`** — Fills `pulumi preview --import-file` skeleton with import IDs from the digest, matching TF modules to Pulumi components via mappings
- **`patch-state`** — Patches imported Pulumi state with field values the cloud API doesn't return (write-only fields, IaC-only defaults, asset sentinels), eliminating post-import diffs
- **`set-secrets`** — Extracts specific secret values from TF state and sets them as Pulumi config secrets

These commands are designed to work together in a pipeline: `tf-digest` → `import-id-match` → `pulumi import` → `patch-state` → zero-diff preview.

For robust approaches to migration please see the
[official documentation](https://www.pulumi.com/docs/iac/guides/migration/migrating-to-pulumi/from-terraform/).

## Upstream: `stack` command (state translation)

The upstream `stack` command translates an entire TF state directly into a Pulumi stack state without using `pulumi import`:

```
$ pulumi plugin run terraform-migrate -- stack --help

Translate Terraform state to a Pulumi stack state.

Example:

  pulumi plugin run terraform-migrate -- stack \
    --from path/to/terraform-sources \
    --to path/to/pulumi-project \
    --out /tmp/pulumi-state.json \
    --plugins /tmp/required-plugins.json

The translated state picks recommended Pulumi providers and resource types to represent every Terraform resource
present in the source.

Before running this tool, '--to path/to/pulumi-project' should contain a valid Pulumi project with a
currently selected stack that already has initial state ('pulumi stack export' succeeds).

Generated 'pulumi-state.json' file is in the format compatible with importing into a Pulumi project:

  pulumi stack import --file pulumi-state.json

Setting the optional '--plugins' parameter generates a 'required-plugins.json' such as '[{"name":"aws", "version":"7.12.0"}]'.
This file recommends Pulumi plugins and versions to install into the project, for example:

  pulumi plugin install resource aws 7.12.0

The tool may run 'tofu', 'tofu init', 'tofu refresh' to extract the Terraform state and these commands may require
authorizing read-only access to the cloud accounts. The tool never runs mutating commands such as 'tofu apply'.

See also:

- pulumi stack import
  https://www.pulumi.com/docs/iac/cli/commands/pulumi_stack_import/

- pulumi plugin install
  https://www.pulumi.com/docs/iac/cli/commands/pulumi_plugin_install/
```

This produces a draft [Pulumi stack state](https://www.pulumi.com/docs/iac/cli/commands/pulumi_state/) that represents
a translated input Terraform state. Additionally it produces a map of recommended Pulumi provider names and versions to
use in the translation.

To proceed with the migration, import the state into your Pulumi stack, feed these artifacts into an LLM, and ask it to
produce Pulumi sources that translate the Terraform sources. Instructing the LLM to aim for a clean `pulumi preview`
helps is to fix discrepancies between code and state and get accurate results.

## Migration workflow

The `tf-digest` and `import-id-match` commands work together to automate Pulumi
resource imports from Terraform state. The end-to-end flow looks like this:

```
 ┌─────────────────┐     ┌──────────────────────┐     ┌──────────────────────┐
 │  TF sources +   │     │  Pulumi program with │     │  Mapping config      │
 │  state          │     │  ComponentResources   │     │  (--map flags or     │
 │                 │     │                       │     │   mappings.yaml)     │
 └────────┬────────┘     └──────────┬────────────┘     └──────────┬───────────┘
          │                         │                             │
          ▼                         ▼                             │
  ┌───────────────┐     ┌───────────────────────┐                 │
  │  tf-digest    │     │  pulumi preview       │                 │
  │               │     │  --import-file        │                 │
  └───────┬───────┘     │  import.json          │                 │
          │             └───────────┬───────────┘                 │
          │                         │                             │
          ▼                         ▼                             ▼
  ┌───────────────┐     ┌───────────────────────┐     ┌───────────────────────┐
  │ tf-digest.json│────▶│   import-id-match      │◀───│  --map / --mapping-   │
  │               │     │                       │     │  file                 │
  └───────────────┘     └───────────┬───────────┘     └───────────────────────┘
                                    │
                                    ▼
                        ┌───────────────────────┐
                        │  filled-import.json   │
                        │  (IDs populated)      │
                        └───────────┬───────────┘
                                    │
                                    ▼
                        ┌───────────────────────┐
                        │  pulumi import        │
                        │  --file filled-       │
                        │  import.json          │
                        └───────────────────────┘
```

### Step-by-step example

```bash
# 1. Digest Terraform sources + state into a sidecar JSON
pulumi plugin run terraform-migrate -- tf-digest \
  --from ./tf-sources \
  --hostname scalr.example.com \
  --organization my-org \
  --workspace my-workspace-dev \
  --token-env SCALR_TOKEN \
  --out tf-digest.json \
  --pulumi-stack dev \
  --pulumi-project myproject

# 2. Generate the skeleton import file from a Pulumi preview
pulumi preview --import-file import.json

# 3. Fill import IDs by matching TF resources → Pulumi components
pulumi plugin run terraform-migrate -- import-id-match \
  --digest tf-digest.json \
  --import-file import.json \
  --map 'module.caas_rds=caas_rds' \
  --map 'module.capture_ui["dmvhm"]=capture_ui["dmvhm"]' \
  --map 'module.lambda_vpc["dmvhm"]=lambda_vpc-dmvhm' \
  --out filled-import.json

# 4. Import resources into the Pulumi stack
pulumi import --file filled-import.json
```

---

## `tf-digest` command

Generates a `tf-digest.json` sidecar file describing Terraform module instances, their interfaces
(inputs/outputs with evaluated values), and the Pulumi URNs of resources belonging to each instance.

```
pulumi-tool-terraform-migrate tf-digest \
  --from path/to/terraform-sources \
  --hostname app.terraform.io \
  --organization my-org \
  --workspace my-workspace \
  --token-env TFC_TOKEN \
  --out /tmp/tf-digest.json \
  --pulumi-stack dev \
  --pulumi-project myproject \
  --project-dir ./pulumi
```

Sensitive attributes in state are automatically discovered and set as encrypted
Pulumi config secrets via `pulumi config set --secret`. Use `--skip-secrets` to
opt out. Config keys are derived from the terraform address
(e.g. `module_rds_dmvhm_aws_db_instance_main_password`).

### `tf-digest` internal flow

```
 ┌──────────────────────────────────────────────────────────┐
 │ INPUTS                                                   │
 │  --from <tf-dir>           Terraform root module dir     │
 │  --state-file <path>   ─┐  State source (pick one)      │
 │  --hostname/org/ws     ─┘  Remote via TFC/Scalr API     │
 │  --pulumi-stack/project    For URN generation            │
 │  --project-dir <path>      Pulumi project dir (default .)│
 │  --skip-secrets            Skip setting config secrets   │
 └────────────────────┬─────────────────────────────────────┘
                      │
                      ▼
 [1] Load Terraform Configuration
     • Parse .tf files and module sources
     • Auto-run tofu init -backend=false if modules not installed
                      │
                      ▼
 [2] Load State
     • Remote: TFC/Scalr API (discovery → workspace → download)
     • Local: read .tfstate from disk
                      │
                      ▼
 [3] Detect Format & Parse
     • Raw .tfstate → statefile.Read()
     • tofu show -json → synthetic state from JSON
                      │
                      ▼
 [4] Create Evaluation Context
     • Discover provider plugins in .terraform/providers/
     • Start providers as subprocesses (schema only, no API calls)
     • Register builtin terraform provider stub
       (terraform_remote_state, terraform_data)
                      │
                      ▼
 [4b] Resolve Pulumi Providers
      • Map terraform provider names → Pulumi providers
      • Used to translate resource addresses to Pulumi URNs
                      │
                      ▼
 [5] Build Root Variable Values
     • Parse terraform.tfvars + *.auto.tfvars
     • Fetch workspace vars from TFC/Scalr API
     • Fill remaining required vars with unknown placeholders
                      │
                      ▼
 [5b] Build Eval Scopes (one-time graph walk)
      • Build OpenTofu eval graph from config + state + vars
      • Walk graph once (resolves all variables, locals, outputs)
      • Cache scopes for all module instances
                      │
                      ▼
 [6] Build TF Digest
     For each module call in config:
     ├─ Discover instances from state (count/for_each keys)
     ├─ Match resources to each instance
     │  ├─ Translate to Pulumi URNs
     │  ├─ Extract import IDs from state
     │  └─ Redact sensitive attrs (from state metadata)
     ├─ Build interface (inputs/outputs from child config)
     │  ├─ Extract call-site HCL expressions
     │  └─ Evaluate variable values via cached scope
     └─ Recurse into nested modules
     Also collect root-level resources
                      │
                      ▼
 [7] Write tf-digest.json
                      │
                      ▼
 [8] Set Secrets (unless --skip-secrets)
     • Discover sensitive attrs via AttrSensitivePaths
     • Flatten terraform address to config key
     • Run `pulumi config set --secret` for each
     • Values never appear in tf-digest.json output
```

---

## `import-id-match` command

Fills a Pulumi import file's `<PLACEHOLDER>` IDs by matching resources from a TF digest
to Pulumi component children. This bridges the naming gap between Terraform's flat
resource addresses and Pulumi's component-based naming.

### Why is this needed?

When `pulumi preview --import-file` generates a skeleton import file, all IDs
are `<PLACEHOLDER>`. The TF digest knows the real import IDs (from state), but
the resource names differ between TF and Pulumi.

The `import-id-match` command solves this by:

1. Grouping import entries by their `parent` field (component children)
2. Grouping TF resources by their module path (from the digest)
3. Using explicit mappings to pair TF modules with Pulumi components
4. Matching children within each pair **by type + resource name**
5. Falling back to type-only matching when there's a single candidate

### Usage

```
pulumi-tool-terraform-migrate import-id-match \
  --digest tf-digest.json \
  --import-file import.json \
  --map 'module.caas_rds=caas_rds' \
  --map 'module.capture_ui["dmvhm"]=capture_ui["dmvhm"]' \
  --mapping-file mappings.yaml \
  --out filled-import.json
```

### Flags

| Flag | Required | Description |
|------|----------|-------------|
| `--digest` | Yes | Path to `tf-digest.json` (from `tf-digest` command) |
| `--import-file` | Yes | Path to Pulumi import file (from `pulumi preview --import-file`) |
| `--map` | No | Repeatable. Format: `module.X=componentName` |
| `--mapping-file` | No | Path to YAML mapping file |
| `--out` / `-o` | Yes | Output path for the filled import file |

At least one of `--map` or `--mapping-file` should be provided (both can be used together;
CLI flags override file entries).

### Mapping format

**CLI flags** (repeatable):
```
--map 'module.caas_rds=caas_rds'
--map 'module.capture_ui["dmvhm"]=capture_ui["dmvhm"]'
--map 'module.lambda_vpc["dmvhm"]=lambda_vpc-dmvhm'
```

- **Left side**: TF module path as it appears in `terraformPath` in the digest
- **Right side**: Pulumi component instance name as it appears in the `name` field of
  component entries in the import file

**Mapping file** (`--mapping-file mappings.yaml`):
```yaml
mappings:
  module.caas_rds: caas_rds
  module.capture_ui["dmvhm"]: capture_ui["dmvhm"]
  module.lambda_vpc["dmvhm"]: lambda_vpc-dmvhm
```

Root-level resources (no module / no parent) are matched automatically without mappings.

### Matching algorithm

The matching is deterministic when Pulumi components use TF resource names
as logical name suffixes (the convention enforced by the component generation
skill):

```
TF digest:   module.caas_rds.aws_rds_cluster.aurora_cluster
                                              ^^^^^^^^^^^^^^ extractResourceName → "aurora_cluster"

Import file: name: "caas_rds-aurora_cluster", parent: "caas_rds"
                            ^^^^^^^^^^^^^^ extractImportSuffix → "aurora_cluster"

Match: type=aws:rds/cluster:Cluster + name="aurora_cluster" → fill ID
```

```
 ┌────────────────────────┐          ┌──────────────────────────┐
 │    tf-digest.json      │          │     import.json          │
 │                        │          │                          │
 │  modules:              │          │  resources:              │
 │    module.caas_rds:    │          │    - type: Component     │
 │      - aws_rds_cluster │          │      name: caas_rds      │
 │        .aurora_cluster │          │      component: true     │
 │        id: cluster-123 │          │                          │
 │      - aws_rds_cluster_│          │    - type: aws:rds/...   │
 │        instance.inst   │          │      name: caas_rds-     │
 │        id: inst-456    │          │        aurora_cluster    │
 │                        │          │      id: <PLACEHOLDER>   │
 │  rootResources:        │          │      parent: caas_rds    │
 │    - aws_s3_bucket     │          │                          │
 │      .my_bucket        │          │    - type: aws:rds/...   │
 │      id: my-bucket     │          │      name: caas_rds-inst │
 └───────────┬────────────┘          │      id: <PLACEHOLDER>   │
             │                       │      parent: caas_rds    │
             │    ┌──────────────┐   │                          │
             │    │   mappings   │   │    - type: aws:s3/...    │
             │    │              │   │      name: my_bucket     │
             │    │ module.      │   │      id: <PLACEHOLDER>   │
             │    │ caas_rds     │   └──────────┬───────────────┘
             │    │  = caas_rds  │               │
             │    └──────┬───────┘               │
             │           │                       │
             ▼           ▼                       ▼
     ┌───────────────────────────────────────────────────┐
     │           import-id-match command                 │
     │                                                   │
     │  1. Group import entries by parent                │
     │     caas_rds → [aurora_cluster, inst]             │
     │     (orphans) → [my_bucket]                       │
     │                                                   │
     │  2. Group TF resources by module path             │
     │     module.caas_rds → [aurora_cluster, inst]      │
     │     root → [my_bucket]                            │
     │                                                   │
     │  3. Pair via mappings                             │
     │  4. Match by type + name (deterministic)          │
     │  5. Root resources matched automatically          │
     └──────────────────────┬────────────────────────────┘
                            │
                            ▼
              ┌──────────────────────────────┐
              │   filled-import.json         │
              │                              │
              │   aurora_cluster: cluster-123 │
              │   inst: inst-456             │
              │   my_bucket: my-bucket       │
              └──────────────────────────────┘
```

---

## `patch-state` command

After `pulumi import`, the cloud API's Read doesn't return all field values. Write-only fields
(passwords, file paths, asset content), IaC-only fields (`forceDestroy`, `applyImmediately`),
and asset/archive fields are missing from the imported state, causing diffs on every preview.

The `patch-state` command fills these fields from the TF digest using a curated fields file
(`aws-import-diff-fields.json`) that lists exactly which fields per resource type need
patching and what their defaults are. This targeted approach only patches fields that are:

1. Not returned by the cloud API on import
2. Actually set by the Pulumi program (e.g., `forceDestroy`, `acl`, `source`)

Building out the fields file requires manual effort per resource type, but the
result is predictable and safe.

### Usage

```bash
pulumi plugin run terraform-migrate -- patch-state \
  --state /tmp/exported-state.json \
  --digest tf-digest.json \
  --fields aws-import-diff-fields.json \
  --mapping-file mappings.yaml \
  --project-dir . --stack dev \
  --config-dir ../environments/develop \
  --out /tmp/patched-state.json
```

### What it patches

| Category | Examples | Source |
|----------|----------|--------|
| IaC-only defaults | `forceDestroy`, `applyImmediately` | Defaults from `aws-import-diff-fields.json` |
| Write-only fields | `masterPassword`, `secretString` | TF digest values (sensitive values resolved from stack config secrets) |
| Asset fields | Lambda `code`, S3 object `source` | File paths from TF config dir, converted to Pulumi asset/archive sentinels |
| Read-filtered | ClusterParameterGroup `parameters` | TF digest (provider Read filters by source) |

### Flags

| Flag | Required | Description |
|------|----------|-------------|
| `--state` | Yes | Path to exported Pulumi state (`pulumi stack export --show-secrets`) |
| `--digest` | Yes | Path to `tf-digest.json` |
| `--fields` | Yes | Path to `aws-import-diff-fields.json` |
| `--mapping-file` | No | Path to `mappings.yaml` (same as `import-id-match`) |
| `--project-dir` | No | Pulumi project dir (for reading config secrets) |
| `--stack` | No | Stack name (for reading config secrets) |
| `--config-dir` | No | TF config dir (for resolving asset file paths) |
| `--out` / `-o` | Yes | Output path for patched state |

### How defaults vs digest values work

The fields file serves two purposes:

1. **TF schema defaults** — The Pulumi-Terraform bridge applies TF SDK defaults (like
   `forceDestroy: false`) during preview via `PlanResourceChange`. After import, these
   fields are nil in the state, causing a diff of `null → false` on every preview. Defaults
   from the fields file are applied to **all** resources of a matching type, regardless of
   whether they have a TF digest match. This is correct because the bridge default issue
   affects every instance of the type.

2. **Digest-specific values** — Fields that the cloud API doesn't return but that hold
   resource-specific values (like Lambda `code` file paths, S3 object `source` paths,
   `secretString` values). These are only patched when the resource is successfully matched
   to a TF digest entry via name mappings, because the values are unique per resource.

The fields file must be manually built out per resource type — resource types not listed
in the file receive no patching at all. The v2 format is flat (no `not_read` wrapper):

```json
{
  "fields": {
    "aws:s3/bucket:Bucket": {
      "forceDestroy": { "default": false }
    },
    "aws:lambda/function:Function": {
      "publish": { "default": false },
      "code": { "asset": "FileArchive", "assetKind": 2, "archiveFormat": 3, "hashField": "source_code_hash" }
    }
  }
}
```

Each field entry supports: `default` (value to apply when nil), `asset` (`"FileAsset"` or
`"FileArchive"`), `assetKind` (bridge enum: 0=FileAsset, 2=FileArchive), `archiveFormat`
(3=ZIPArchive), `hashField` (TF attr for source code hash). Fields without a `default` are
only patched when a digest value is found.

### Patching pipeline

For each resource in the state:

1. **Look up fields** by resource type in the fields file (skip if type not listed)
2. **Try to match** the resource to a TF digest entry via name mappings (optional — defaults
   still apply without a match)
3. **Iterate** the fields listed for this type
4. **Patch inputs**: if the state input is nil/empty, fill from the digest value (if matched)
   or the field's default
5. **Build asset sentinels**: for `FileAsset`/`FileArchive` fields, convert TF file paths to
   Pulumi asset/archive sentinels with SHA-256 hashes (falls back to downloading Lambda code
   from AWS when local files aren't available — requires AWS credentials via
   `pulumi env run oidc-vic/dev --`)
6. **Resolve secrets**: for sensitive fields redacted in the digest, look up decrypted values
   from Pulumi stack config and wrap in secret sentinels
7. **Patch outputs**: mirror simple values and asset sentinels to outputs (the bridge uses
   outputs to reconstruct TF state for diffing)
8. **Update delta**: inject asset delta entries into `__pulumi_raw_state_delta` so the bridge
   can correctly reverse asset translations during Diff
9. **Validate with Recover**: run the bridge's `Recover` function against the patched outputs
   to verify compatibility with the delta. If Recover fails, **revert both inputs and outputs**
   to pre-patch values and log a warning. This prevents provider panics at preview time.

### Delta and Recover validation

The Pulumi-Terraform bridge stores a `__pulumi_raw_state_delta` in each resource's outputs
that encodes how to reconstruct TF raw state from Pulumi PropertyValues. During Diff, the
bridge calls `Recover(oldOutputs)` to reverse this transformation. If patched outputs are
incompatible with the delta (e.g., a string where the delta expects an object), Recover
fails and the provider panics.

The patcher validates every patched resource by running `Recover` inline after patching.
On failure, it reverts both inputs and outputs to their pre-patch values (keeping them
consistent) and increments the `Delta FAILED` counter. The resource retains its original
imported state — it may show phantom diffs on the next preview but will not panic. After a
successful `pulumi up`, the bridge recomputes a correct delta.
