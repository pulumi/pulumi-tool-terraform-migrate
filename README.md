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

## `module-map` command

Generates a `module-map.json` sidecar file describing Terraform module instances, their interfaces
(inputs/outputs with evaluated values), and the Pulumi URNs of resources belonging to each instance.

```
pulumi-tool-terraform-migrate module-map \
  --from path/to/terraform-sources \
  --hostname app.terraform.io \
  --organization my-org \
  --workspace my-workspace \
  --token-env TFC_TOKEN \
  --out /tmp/module-map.json \
  --pulumi-stack dev \
  --pulumi-project myproject \
  --project-dir ./pulumi
```

Sensitive attributes in state are automatically discovered and set as encrypted
Pulumi config secrets via `pulumi config set --secret`. Use `--skip-secrets` to
opt out. Config keys are derived from the terraform address
(e.g. `module_rds_dmvhm_aws_db_instance_main_password`).

### Flow

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
 [6] Build Module Map
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
 [7] Write module-map.json
                      │
                      ▼
 [8] Set Secrets (unless --skip-secrets)
     • Discover sensitive attrs via AttrSensitivePaths
     • Flatten terraform address to config key
     • Run `pulumi config set --secret` for each
     • Values never appear in module-map.json output
```
