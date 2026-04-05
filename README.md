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

## Module-to-Component Translation

By default, the tool translates Terraform module hierarchy into Pulumi `ComponentResource` entries in the migrated state. Each Terraform module becomes a component resource with parent-child relationships matching the original module nesting.

### Module Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--no-module-components` | `false` | Disable component generation entirely. All resources are flattened under the Stack with module paths baked into resource names (legacy behavior). |
| `--state-file` | none | Path to a pre-captured state file (`tofu show -json` output or `terraform.tfstate`). Bypasses running tofu commands. Use when state is captured separately from the HCL source directory. |
| `--component-inputs` | `true` | Populate component inputs in translated state. Set to `true` when generated code will use **component providers** (e.g., `pulumi-terraform-module` plugin, IDP-registered components). Set to `false` for **single-language `ComponentResource` classes**, which historically send empty inputs. |
| `--module-type-map` | none | Override the auto-derived type token for a module. Repeatable. Format: `module.name=pkg:mod:Type`. Applies to all `for_each`/`count` instances of the module. |
| `--module-source-map` | none | Map a module to its HCL source path when auto-discovery can't find it (remote modules, non-standard layouts). Repeatable. Format: `module.name=./path`. |
| `--module-schema` | none | Provide a Pulumi package schema JSON for component interface validation. Repeatable. Format: `module.name=./path/to/schema.json`. When provided, the schema is the source of truth — mismatches between parsed HCL and schema produce errors. |

### How It Works

1. **Component tree**: The tool walks Terraform resource addresses, extracts module paths, and builds a tree of component resources with auto-derived type tokens (e.g., `module.vpc` → `terraform:module/vpc:Vpc`).

2. **HCL parsing**: When TF source files are available (via `--from`), the tool parses module call sites, evaluates argument expressions using the full Terraform function library, and populates component inputs and outputs.

3. **Input population**: Call-site argument expressions are evaluated against an HCL context populated with `terraform.tfvars` + `*.auto.tfvars`, resource attributes from TF state, data source attributes, `locals` values, `module.*` cross-references, `path.*` variables, and `count.index`/`each.key` meta-arguments. Variable defaults are merged for any argument not explicitly passed at the call site. Remote module sources are resolved from the `.terraform/modules/` cache after `tofu init`.

4. **Output population**: Output `value` expressions from HCL are evaluated using the module's child resource attributes from state. Falls back to recording output names with empty values when evaluation fails.

5. **Schema metadata**: A `component-schemas.json` sidecar file is always written alongside the state output. It contains Pulumi-typed component interfaces (input/output names, types, defaults, descriptions) for the code generation agent. Types use Pulumi package schema format (e.g., `"string"`, `{"type": "array", "items": "string"}`).

5. **Schema validation**: When `--module-schema` is provided, the parsed component interface is validated against the schema. Missing required inputs or extra outputs produce descriptive errors.

### Examples

Translate with component resources (default):
```
pulumi-terraform-migrate stack --from ./tf --to ./pulumi --out state.json
```

Translate for single-language components (empty inputs + sidecar schema):
```
pulumi-terraform-migrate stack --from ./tf --to ./pulumi --out state.json \
  --component-inputs=false
```

Override module type tokens:
```
pulumi-terraform-migrate stack --from ./tf --to ./pulumi --out state.json \
  --module-type-map module.vpc=myinfra:network:Vpc \
  --module-type-map module.rds=myinfra:database:Rds
```

Flat mode (no components):
```
pulumi-terraform-migrate stack --from ./tf --to ./pulumi --out state.json \
  --no-module-components
```
