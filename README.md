# pulumi-tool-terraform-migrate

This is an EXPERIMENTAL tool for assisting migrating Terraform projects to Pulumi.

For robust approaches to migration please see the
[official documentation](https://www.pulumi.com/docs/iac/guides/migration/migrating-to-pulumi/from-terraform/).

## Usage

This tool is useful in pipelines that given a Terraform project with sources and state aim to produce equivalent Pulumi
sources and state tracking the same infrastructure. Crucially such pipelines should not do any write operations on the
actual infrastructure, staying in the purely symbolic exploratory world.

The key command translates an entire stack:

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
