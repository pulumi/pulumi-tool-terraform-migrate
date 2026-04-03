# Terraform Modules to Pulumi Component Resources

**Date**: 2026-03-31
**Status**: All PRs (1-10) implemented on `feat/mc-*` branches. Pending: skill update (Task 16), PR reviews, merge.

## Problem

When translating Terraform state to Pulumi state, resources created by Terraform modules are currently flattened — module hierarchy is baked into resource names (e.g., `module.vpc.aws_subnet.this` becomes `vpc_this`) and all resources are direct children of the Pulumi Stack. There is no representation of the module boundary in Pulumi state.

This is a problem because:

1. **Code generation alignment**: Generated Pulumi code should use `ComponentResource` classes to represent modules. If the translated state doesn't have matching component resources as parents, `pulumi preview` will show diffs.
2. **State fidelity**: The migrated Pulumi state should structurally mirror the Terraform module hierarchy.
3. **Operational clarity**: Users need to see which resources came from which module via parent-child relationships in the Pulumi resource tree.

## Background Research

### Terraform Module Inputs and Outputs

Terraform modules have inputs (`variable` blocks) and outputs (`output` blocks), analogous to Pulumi component constructor args and public properties:

```hcl
# modules/vpc/variables.tf — module inputs
variable "cidr" { type = string }
variable "name" { type = string }

# modules/vpc/main.tf — child resources consume inputs
resource "aws_vpc" "this" {
  cidr_block = var.cidr
  tags       = { Name = var.name }
}

# modules/vpc/outputs.tf — module outputs
output "vpc_id" { value = aws_vpc.this.id }
```

Callers pass inputs at the call site:
```hcl
module "vpc" {
  source = "./modules/vpc"
  cidr   = "10.0.0.0/16"
  name   = "production"
}
```

**Critical findings about Terraform state:**

- **Module inputs are NOT in state** — neither the JSON format (`tfjson.StateModule`) nor the raw format (`opentofu/states.Module`) stores variable values. They are resolved at plan/apply time and baked into child resource attributes. To recover input values, we must parse the HCL call site and evaluate expressions.

- **Module outputs ARE in raw state** — `opentofu/states.Module` has an `OutputValues` field (`map[string]*OutputValue`) containing the resolved output values as `cty.Value`. However, these are NOT available in the JSON state format (`tfjson.StateModule` only has `Resources`, `ChildModules`, `Address`).

- **Module interface (variable/output declarations)** — the names, types, and descriptions of inputs and outputs are only in HCL source files, not in state. We need these for code generation regardless of where values come from.

### What Pulumi Stores in Component Resource State

Research into the Pulumi engine (`pulumi/pulumi`) revealed two distinct behaviors depending on the component type:

#### Single-language components (`custom=false`, `remote=false`)

These are inline `ComponentResource` subclasses written in the same language as the Pulumi program.

**Before Pulumi v3.202.0 (Oct 2025):** SDKs did NOT send component inputs to the engine:
- Go SDK: `RegisterComponentResource()` explicitly passes `nil` for props
- Node.js SDK: Replaces `args` with `{}` for non-remote components
- Python SDK: Always sent props (the exception)

**After v3.202.0 (PR #20357):** SDKs now send inputs, but an escape hatch exists (`PULUMI_NODEJS_SKIP_COMPONENT_INPUTS`). Most existing stacks still have empty inputs.

**Result:** Single-language component inputs in state are typically `{}` or `nil`.

#### Component providers / remote components (`custom=false`, `remote=true`)

These are multi-language components served via a provider's `Construct` RPC (e.g., `pulumi-terraform-module` plugin, `@pulumi/awsx`). All SDKs have always sent inputs for remote components — they're needed for the `Construct` call.

**Result:** Remote component inputs in state are always populated.

#### Common behavior

1. **Outputs ARE stored** — Set by `registerOutputs()`. Both component types store outputs.

2. **Diff is `DeepEquals` on inputs only** — On `pulumi preview`, the engine compares old inputs vs new inputs using a simple `DeepEquals` (no provider involvement for components). If they don't match, a diff/update is shown. **Outputs are NOT compared during diff.** (Source: `step_generator.go:2698-2703`)

3. **No provider involvement** — Components have no provider, so no `Check`, `Create`, `Diff`, or `Update` RPCs. The engine just stores what the program sends.

#### Implications for state translation

- **Component providers (remote=true):** Translated state SHOULD have populated inputs — the generated code will send inputs via `Construct`, and they must match for a clean preview.
- **Single-language components (remote=false):** Translated state should have **empty inputs `{}`** — the SDK will send `{}`, so populated inputs would cause a spurious diff. The component interface (variable names, types, defaults) should be emitted as a **sidecar metadata file** for the code generator.
- **Outputs:** Always populated regardless of component type (from raw state when available).
- The `--component-inputs` flag controls this behavior at translation time.

### Why HCL Parsing is Required

HCL parsing serves three purposes:

1. **Component interface extraction** (always needed) — `variable` blocks define constructor args, `output` blocks define public properties. This tells the code generator what the component class signature should look like. Only available in HCL source files.

2. **Input value resolution** (needed when `--component-inputs=true`) — Parse call site expressions and evaluate them against TF state + tfvars to get concrete values. Input values at call sites can be complex HCL expressions (`var.cidr`, `module.other.output`, `join(...)`, conditionals), requiring a full HCL expression evaluator with access to the Terraform function library.

3. **Schema metadata generation** (always emitted) — A `component-schemas.json` sidecar file is always written alongside the translated state. It contains the component interface for each module (variable names, Pulumi-formatted types, defaults, output names). The code generation agent uses this for type-correct code regardless of whether inputs are in state. Pulumi state itself carries no type information (`inputs`/`outputs` are `map[string]any`).

**Output values are evaluated from HCL** — TF state v4 format does NOT persist module-level output values (only root outputs and resource attributes). Module outputs are computed at runtime during plan/apply and not serialized. Output `value` expressions are evaluated from HCL using the module's child resource attributes from state as the eval context.

### Why Custom HCL Parsing (Not Reusing Existing Pulumi Code)

The Pulumi ecosystem has extensive HCL parsing infrastructure:

- **`codegen/pcl`** (Pulumi SDK) — binds HCL syntax to Pulumi's type system for *code generation*. `model.BindExpression()` produces typed AST nodes (`model.Expression`), not concrete values.
- **`tf2pulumi/convert`** (Terraform Bridge) — converts TF HCL *syntax* to PCL syntax. It's a syntax-to-syntax transformer with an intermediate language (`il/graph.go` with `ModuleNode`, `LocalNode`, etc.).

**Neither serves our use case.** We need to evaluate HCL expressions to *concrete values* (strings, numbers, objects) using TF state data as the eval context, then insert those values into Pulumi component state.

The PCL binder's `model.Expression` does have an `.Evaluate(context)` method that produces `cty.Value`, but it delegates to the same `hcl.Expression.Value(ctx)` call we use directly. Using the PCL binder would add a binding/type-checking layer that requires constructing `model.Scope` definitions from TF state — extra work for the same result. The binder is designed for static analysis of Pulumi programs, not evaluation with runtime data from TF state.

Our custom `pkg/hcl/` package (~1600 lines) uses the correct foundation:
- `hashicorp/hcl/v2` for parsing (same as all Pulumi/Terraform tools)
- `opentofu/lang.Scope.Functions()` for the full Terraform function library
- `hcl.Expression.Value(ctx)` for direct expression evaluation

The eval context is populated with data that neither PCL nor tf2pulumi provides: resource attributes from TF state JSON, data source attributes, resolved locals, module cross-references, tfvars values, and path references.

**Type information for the code generation agent** is provided separately via `component-schemas.json` — a sidecar metadata file always emitted alongside the translated state. HCL type constraints from `variable` blocks (e.g., `string`, `list(string)`, `map(number)`) are converted to Pulumi package schema type format via a simple string converter (`hclTypeToPulumiSchemaType`). This gives the agent Pulumi-native types without requiring the full PCL binder integration. Pulumi state itself carries no type information (inputs/outputs are `map[string]any`).

**Why the type conversion is simple here but complex in codegen:** Our converter handles *HCL type constraints* — the explicit `type = list(string)` declarations in `variable` blocks. These are a small, well-defined grammar that maps directly to Pulumi schema types via string parsing. Codegen's type system solves a fundamentally harder problem: *expression type inference* — determining what type `join("-", [var.a, var.b])` returns, unifying types across conditional branches, resolving generic types like `Output<T>`, and computing types from provider schemas. We skip all of this because we always evaluate expressions to concrete values using TF state as the eval context — the JSON serialization determines the representation. The only place we need declared types is `variable` block constraints, which are explicitly written in HCL source.

## Design

### Phase 1: Component Resource Structure

#### Component Resource Creation

When the tool encounters a Terraform resource with a module address (e.g., `module.vpc.module.subnets.aws_subnet.this`), it will:

1. Parse the module path into segments: `["vpc", "subnets"]`.
2. For each segment, ensure a corresponding component resource exists in the Pulumi state.
3. Set the parent chain: `Stack -> vpc component -> subnets component -> subnet resource`.

Component resources are `ResourceV3` entries with:

| Field | Value |
|-------|-------|
| `custom` | `false` |
| `type` | Derived from module name (default) or overridden via config |
| `urn` | Standard format: `urn:pulumi:<stack>::<project>::<type>::<name>` |
| `parent` | Parent component's URN, or Stack URN for top-level modules |
| `inputs` | Empty `{}` in Phase 1 |
| `outputs` | Empty `{}` in Phase 1 |
| `id` | Empty (components aren't physical resources) |
| `provider` | Empty (components don't have providers) |

Multiple TF resources in the same module (e.g., `module.vpc.aws_subnet.a` and `module.vpc.aws_route_table.rt`) share a single component resource. The tool tracks which components have been created during traversal to deduplicate.

#### Full Module Nesting

Nested modules produce a full component hierarchy. Pulumi URNs encode the parent type chain using `$` delimiters. For `module.vpc.module.subnets.aws_subnet.this`:

```
Stack (urn:pulumi:dev::project::pulumi:pulumi:Stack::project-dev)
  └── vpc (urn:pulumi:dev::project::terraform:module/vpc:Vpc::vpc)
        └── subnets (urn:pulumi:dev::project::terraform:module/vpc:Vpc$terraform:module/subnets:Subnets::subnets)
              └── this (urn:pulumi:dev::project::terraform:module/vpc:Vpc$terraform:module/subnets:Subnets$aws:ec2/subnet:Subnet::this)
```

The existing `makeUrn` function will need to be extended to accept a parent type chain and encode it in the URN using `$` delimiters, matching how the Pulumi SDK constructs URNs for nested resources.

#### Type Token Format

Default type token: `terraform:module/<moduleName>:<ModuleName>`

**Naming algorithm**: Split the module name on underscores, capitalize the first character of each segment (non-alpha leading characters are left as-is), join for PascalCase (type name) and lowercase-first for camelCase (module path segment). Module names starting with a digit (e.g., `module.2vpc`) produce type names starting with a digit which may be invalid in some SDKs — the tool logs a warning and proceeds; users should provide a type mapping override for these cases.

Examples:
- `module.vpc` -> `terraform:module/vpc:Vpc`
- `module.s3_bucket` -> `terraform:module/s3Bucket:S3Bucket`
- `module.my_vpc_v2` -> `terraform:module/myVpcV2:MyVpcV2`
- `module.s3` -> `terraform:module/s3:S3`
- `module.vpc.module.subnets` -> the `subnets` component gets `terraform:module/subnets:Subnets`

This follows Pulumi's `pkg:module:Type` convention.

#### Resource Naming

With component resources enabled, resource names strip the module prefix since hierarchy is expressed via the parent chain:

- `module.vpc.aws_subnet.this` -> name `this` (parent is `vpc` component)
- `module.vpc.module.subnets.aws_subnet.this` -> name `this` (parent is `subnets` component)

This avoids the current behavior of baking module paths into names (`vpc_subnets_this`).

Resource name uniqueness is guaranteed by the (type, name, parent) tuple in Pulumi. Two resources with the same name but different types or parents do not collide.

#### Indexed and Keyed Modules

Terraform supports both `count` (numeric indices) and `for_each` (string keys) on modules.

**Numeric indices** (`module.vpc[0]`, `module.vpc[1]`): Each index gets its own component resource instance with a unique name (e.g., `vpc-0`, `vpc-1`) but the same type token.

**String keys** (`module.vpc["us-east-1"]`, `module.buckets["logs"]`): Each key gets its own component with a name derived from the key (e.g., `vpc-us-east-1`, `buckets-logs`). Characters invalid in Pulumi resource names are sanitized (replace non-alphanumeric characters with `-`, collapse consecutive `-`, trim leading/trailing `-`). If sanitization produces duplicate names (e.g., `module.vpc["us-east-1"]` and `module.vpc["us_east_1"]` both sanitize to `vpc-us-east-1`), the tool errors with a descriptive message.

**Non-indexed** (`module.vpc`): A single component with name matching the module name (e.g., `vpc`). No "base" component is created for indexed/keyed modules — only the individual instances exist.

Type mappings apply to the base module path (`module.vpc`) and match all instances regardless of index or key.

**Worked example** — deeply nested indexed modules:

`module.clusters[0].module.services["api"].aws_lambda_function.handler` produces:

```
Stack
  └── clusters-0 (terraform:module/clusters:Clusters)
        └── services-api (terraform:module/clusters:Clusters$terraform:module/services:Services)
              └── handler (terraform:module/clusters:Clusters$terraform:module/services:Services$aws:lambda/function:Function)
```

### Type Mapping Overrides

#### Default Behavior

Type tokens are derived automatically as `terraform:module/<name>:<Name>`. No configuration required.

#### Migration File Configuration

Add a `Modules` field to the `Stack` struct:

```json
{
  "migration": {
    "stacks": [{
      "tf-state": "terraform.tfstate",
      "pulumi-stack": "dev",
      "modules": [
        {
          "tf-module": "module.vpc",
          "pulumi-type": "myproject:index:VpcComponent"
        },
        {
          "tf-module": "module.vpc.module.subnets",
          "pulumi-type": "myproject:network:SubnetGroup"
        }
      ],
      "resources": [...]
    }]
  }
}
```

#### CLI Flag Override

For quick one-offs without a migration file:

```
--module-type-map module.vpc=myproject:index:VpcComponent
```

Repeatable flag for multiple mappings. CLI flags take precedence over migration file entries.

#### Matching Rules

- Exact match on the full module path (e.g., `module.vpc.module.subnets`).
- For indexed/keyed modules, the mapping applies to all instances — map `module.vpc` and it matches `module.vpc[0]`, `module.vpc["us-east-1"]`, etc.
- **Precedence chain**: CLI flag > migration file > auto-derived default.

### Component Input Mode (`--component-inputs`)

Controls whether component inputs are populated in translated state. This flag exists because single-language components and component providers handle inputs differently (see Background Research).

#### `--component-inputs=true` (default)

Populate component inputs in translated state from HCL call-site expression evaluation + variable defaults. Use this when the generated Pulumi code will use **component providers** (remote components), e.g., wrapping TF modules via `pulumi-terraform-module` or registering with IDP.

- Inputs written to component `ResourceV3.Inputs` in state

#### `--component-inputs=false`

Leave component inputs as empty `{}` in translated state. Use this when the generated Pulumi code will use **single-language ComponentResource classes** (inline components).

- Component `ResourceV3.Inputs` = `{}` in state

#### Sidecar metadata file (`component-schemas.json`)

**Always written** alongside the state output file when HCL sources are available and modules are present. The code generation agent always benefits from typed component interfaces, regardless of whether inputs are in the state. Pulumi state carries no type information.

Types use **Pulumi package schema format**: `"string"`, `"number"`, `"boolean"` for primitives; `{"type": "array", "items": {"type": "string"}}` for `list(string)`; `{"type": "object", "additionalProperties": {"type": "string"}}` for `map(string)`. HCL type constraints from `variable` blocks are converted automatically.

```json
{
  "components": {
    "module.vpc": {
      "type": "terraform:module/vpc:Vpc",
      "source": "./modules/vpc",
      "inputs": [
        {"name": "cidr", "type": "string", "required": true},
        {"name": "name", "type": "string", "required": true},
        {"name": "enable_dns", "type": "boolean", "default": true},
        {"name": "private_subnets", "type": {"type": "array", "items": "string"}}
      ],
      "outputs": [
        {"name": "vpc_id", "type": "string"},
        {"name": "cidr_block", "type": "string"}
      ]
    }
  }
}
```

The code generator agent consumes this file to know:
- What constructor args to declare (from `inputs`)
- Which args are required vs have defaults
- What to pass to `registerOutputs()` (from `outputs`)

#### Outputs are always populated

Regardless of `--component-inputs`, component outputs are populated from raw `.tfstate` `Module.OutputValues` when available. Both single-language and remote components store outputs via `registerOutputs()`.

### Component Schema Validation

#### Default Behavior (No Schema)

When no schema is provided, component inputs and outputs are derived from HCL module `variable`/`output` blocks and TF state. This is the existing Phase 2 path and remains the default.

#### Schema-Validated Behavior

When a Pulumi package schema JSON is provided for a module, the schema is the **source of truth** for the component interface. The tool still parses HCL/state to get **values**, but validates that the parsed inputs/outputs match the schema's **shape**. If they don't match, the tool fails with a descriptive error.

This supports two key scenarios:
- **Existing Pulumi components** — Target Pulumi programs may already have component classes defined. The user provides the schema (e.g., via `pulumi package get-schema`) and the tool validates compatibility.
- **OSS/third-party TF modules** — Public modules (e.g., `terraform-aws-modules/vpc/aws`) can be wrapped via a `terraform-module` plugin or mapped to a custom Pulumi component. Both produce the same artifact: a Pulumi package schema JSON.

Schema uses the standard Pulumi package schema JSON format (`pulumi package get-schema` output). Loaded via `github.com/pulumi/pulumi/pkg/v3/codegen/schema.ImportSpec()` — already available in `go.mod` (v3.222.0):

```go
import "github.com/pulumi/pulumi/pkg/v3/codegen/schema"

var spec schema.PackageSpec
json.Unmarshal(data, &spec)
pkg, err := schema.ImportSpec(spec, nil, schema.ValidationOptions{})

resource, ok := pkg.GetResource("myproject:index:VpcComponent")
// resource.IsComponent == true
// resource.InputProperties — []*Property (inputs)
// resource.Properties — []*Property (outputs)
// property.Name, property.Type.String(), property.IsRequired()
```

No new dependencies needed.

#### Migration File Configuration

Add `schema-path` to the module entry:

```json
{
  "tf-module": "module.vpc",
  "pulumi-type": "myproject:index:VpcComponent",
  "schema-path": "./schemas/vpc-component.json"
}
```

#### CLI Flag

`--module-schema module.vpc=./schemas/vpc-component.json` (repeatable)

**Precedence:** CLI flag > migration file > none (HCL-derived default)

#### Validation Rules

- Schema is **optional** — default is HCL-derived interface
- When provided, schema is **source of truth** for the component interface (field names, required fields)
- Tool still parses HCL/state to get **values** — schema validates the **shape** (field presence)
- **Type compatibility is not validated here** — it is handled by the existing value conversion pipeline (`cty.Value` → `resource.PropertyMap` via `tfbridge`), the same path used for custom resources. Pulumi component state stores values as `PropertyMap`, not type declarations.
- Mismatch = error (not silent override). Examples:
  - Parsed HCL missing an input the schema requires → error
  - Parsed HCL has output not in schema → error
- How the schema was produced is the agent's concern — the tool only consumes it

### State Translation Pipeline Changes

#### Current Flow

1. `VisitResources` flattens all module resources.
2. Each resource gets a name via `PulumiNameFromTerraformAddress` (bakes module path into name).
3. All resources are inserted with `Parent: Stack`.

#### New Flow

1. **Module tree pass**: Before converting resources, walk the `ChildModules` tree and build a map of module paths to component resources. Each component gets a URN, type token (derived or overridden), and parent reference. Modules whose subtree contains no managed resources (only data sources or locals) are skipped — no component is created for empty modules. The tool validates that no two components share the same (type, name, parent) tuple and errors if a collision is detected.
2. **Insert components**: After provider resources but before custom resources, insert component `ResourceV3` entries in depth-first order (parent components before their children). Siblings at the same depth are ordered alphabetically by module name for deterministic output.
3. **Resource conversion**: `VisitResources` stays the same, but during insertion:
   - Parse the resource's module path from its TF address.
   - Set `Parent` to the innermost component's URN instead of Stack.
   - Use short resource name (strip module prefix).
4. **Backward compatibility**: `--no-module-components` flag preserves the current flat behavior. If both `--no-module-components` and `--module-type-map` are specified, the tool warns that type mappings are ignored in flat mode.

#### Key Code Changes (Phase 1)

| File | Change |
|------|--------|
| `pkg/pulumi_state.go` | `PulumiResource` gains a `Parent` field; `PulumiState` gains `Components []PulumiResource` field |
| `pkg/pulumi_state.go` | `InsertResourcesIntoDeployment` handles `Custom: false` for components, inserts between providers and resources |
| `pkg/pulumi_state.go` | `makeUrn` extended to accept parent type chain and encode with `$` delimiters |
| `pkg/state_adapter.go` | New function to build component tree from `tfjson.StateModule` hierarchy |
| `pkg/state_adapter.go` | `PulumiNameFromTerraformAddress` uses short name when components enabled, full path in flat mode |
| `pkg/migration/migration.go` | Add `Module` struct and `Modules []Module` field on `Stack` |
| `cmd/stack.go` | Add `--module-type-map` and `--no-module-components` flags |

### Phase 2: Module HCL Parsing and Input/Output Population

#### Goal

Populate component resource inputs and outputs so that generated Pulumi component code produces state that matches the translated state exactly, yielding a clean `pulumi preview`.

#### OSS Module Strategy

Terraform programs commonly use public/third-party modules (e.g., `terraform-aws-modules/vpc/aws`). The tool supports these via two strategies — chosen by the agent/user, not the tool:

1. **terraform-module plugin** — The agent wraps the TF module as a Pulumi component using the `terraform-module` provider plugin, then generates a schema via `pulumi package get-schema`.
2. **Custom/simplified component** — The agent maps the module to an existing Pulumi package or generates a new component class, producing a schema from source code or HCL analysis.

Both strategies produce the same artifact: a **Pulumi package schema JSON** passed to the tool for validation. The tool does not choose between strategies — it consumes the schema. OSS module strategy is agent-driven.

The tool's contract is: optionally accept a Pulumi package schema JSON for validation; always derive values from HCL/state.

#### Why This Matters

As documented in the Background Research section: component inputs must match exactly between translated state and generated code for a clean preview (diff is `DeepEquals` on inputs). Outputs don't cause diffs but should be populated for fidelity. Module inputs/outputs are not in TF state, so HCL parsing is required.

#### HCL Source Resolution

The tool needs to find the `.tf` files for each module. Sources specified via:

1. **Migration file** — Add `hcl-source` to the module entry:
   ```json
   {
     "tf-module": "module.vpc",
     "pulumi-type": "myproject:index:VpcComponent",
     "hcl-source": "./modules/vpc"
   }
   ```

2. **CLI flag** — `--module-source-map module.vpc=./modules/vpc` (repeatable).

3. **Auto-discovery** — Parse root `module` blocks in `.tf` files to extract `source` attributes:
   - **Local paths** (`./modules/vpc`): resolve relative to TF source directory.
   - **Registry/git sources**: require explicit mapping via migration file or CLI flag (fetching remote modules is a stretch goal).
   - **Note**: Terragrunt-based projects declare modules in `.hcl` files, not `.tf` files. Terragrunt users must provide explicit source mappings via the migration file or CLI flag.

#### Three Parsing Tasks

1. **Module definition** (e.g., `modules/vpc/variables.tf`, `outputs.tf`): Extract `variable` blocks (inputs — name, type, default, description) and `output` blocks (outputs — name, value expression).

2. **Module call site** (e.g., root `main.tf`): Extract argument expressions passed to each `module` block (e.g., `cidr = var.cidr_block`, `name = "production"`).

3. **Expression evaluation**: Resolve expressions to concrete values using an HCL eval context populated with data from TF state and tfvars.

#### Expression Evaluation

Use the `hashicorp/hcl/v2` Go library for expression evaluation. HCL's evaluator natively handles literals, variable references, conditionals, and `for` expressions. For **function calls** (e.g., `join(...)`, `cidrsubnets(...)`), the evaluator looks up functions by name in `hcl.EvalContext.Functions` — we must populate this map with the full Terraform function table.

**Function table sources** (both are already importable dependencies — no submodule needed):
- `github.com/pulumi/opentofu/lang/funcs` — Terraform-specific functions: `cidrsubnets`, `cidrhost`, `templatefile`, `bcrypt`, `timestamp`, `parseint`, etc. (60+ functions)
- `github.com/zclconf/go-cty/cty/function/stdlib` — Standard functions: `join`, `split`, `upper`, `lower`, `length`, `flatten`, `merge`, `keys`, `values`, `regex`, `jsonencode`, `jsondecode`, etc. (80+ functions)

**Eval context population** — build `hcl.EvalContext` with:

- `Variables["var"]` — from `terraform.tfvars` or variable defaults
- `Variables["<resource_type>"]` — resource attributes from TF state (e.g., `aws_vpc.this.id`)
- `Variables["module"]` — module output references from TF state (e.g., `module.vpc.vpc_id`)
- `Functions` — combined function table from both libraries above

Expression types that work without any function library (handled natively by HCL evaluator):
- Literals (`"10.0.0.0/16"`)
- Variable references (`var.cidr`)
- Conditionals (`var.enable ? "yes" : "no"`)
- `for` expressions (`[for s in var.list : s]`)
- Arithmetic, comparison, logical operators

Expression types that require the function table:
- Function calls (`join("-", ["a", "b"])`, `cidrsubnets(var.cidr, 4, 4)`)

**Fallback**: If an expression can't be evaluated (unregistered function, missing variable, etc.), log a warning and omit that field from component state. The user can address it manually or via the migration file.

#### Component State Population

- **Inputs**: Evaluated values from module call site arguments. These must exactly match what the generated Pulumi component code will pass as constructor args. Source: HCL call site parsing + expression evaluation.
- **Outputs**: Read directly from `opentofu/states.Module.OutputValues` in the raw `.tfstate` file. Fallback: evaluate output expressions from HCL if raw state is unavailable (e.g., JSON-only input). Set via `registerOutputs()` in generated code.

#### Schema Validation Step

After HCL parsing produces inputs/outputs and before populating component state:

1. Parse HCL to get input/output **values** (always, when source available)
2. Read module output **values** from raw `.tfstate` (when available)
3. If schema provided → validate parsed interface matches schema → fail on mismatch
4. If no schema → parsed HCL interface is authoritative
5. Populate component state with values

#### Key Code Changes (Phase 2)

| File | Change |
|------|--------|
| `pkg/hcl/` (new package) | HCL parsing, module definition extraction, expression evaluation |
| `pkg/component_schema.go` | Load Pulumi package schema, extract component interface, validate against parsed HCL |
| `pkg/state_adapter.go` | Populate component inputs/outputs from parsed HCL |
| `pkg/migration/migration.go` | Add `HCLSource` and `SchemaPath` fields to `Module` struct |
| `cmd/stack.go` | Add `--module-source-map` and `--module-schema` flags |

## Testing Strategy

### Ground Truth Test Data

Test expected state is generated from real deployments, not hand-crafted:

1. Write real Terraform HCL with modules (variables, outputs, child resources).
2. `tofu init && tofu apply` to deploy real infrastructure.
3. Capture the resulting TF state as test fixtures.
4. Write equivalent Pulumi code with component resources matching the module structure.
5. `pulumi up` to deploy the same infrastructure.
6. Export Pulumi state via `pulumi stack export` as expected output.
7. Compare translated state against the real Pulumi state.

Use providers that don't require cloud credentials for CI:
- `random` provider (`random_id`, `random_string`, `random_pet`)
- `null` provider (`null_resource` with triggers)
- `local` provider (`local_file`)

### Phase 1 Tests

**Unit tests** (`pkg/state_adapter_test.go`):
- Single module -> one component resource with correct type token, parent=Stack, child resources parented to component.
- Two sibling modules -> two components, each with correct children.
- Nested modules -> component chain (Stack -> vpc -> subnets), resources parented to innermost component.
- Indexed modules (`module.vpc[0]`, `module.vpc[1]`) -> separate component instances, shared type token.
- String-keyed modules (`module.vpc["us-east-1"]`) -> component names sanitized, correct type token.
- Deeply nested indexed modules (`module.clusters[0].module.services["api"]`) -> full component chain with correct URNs.
- Type mapping override -> component uses specified type instead of derived one.
- `--no-module-components` flag -> flat behavior preserved (backward compat).
- Resources in root module (no module prefix) -> still parented to Stack directly.
- Empty modules (only data sources, no managed resources) -> no component created.

**State structure tests** (`pkg/pulumi_state_test.go`):
- Component resources have `Custom: false`, no `ID`, no `Provider`.
- Insertion order: Stack -> providers -> components (depth-ordered) -> custom resources.
- URNs are correctly formed for components.

**Integration tests** (`test/translate_test.go`):
- End-to-end: TF state with modules -> translated Pulumi state -> `pulumi stack import` succeeds.
- Use existing nested module test data and newly generated ground truth fixtures.

### Phase 2 Tests

**HCL parsing tests**:
- Parse `variable` blocks -> correct input schema (name, type, default).
- Parse `output` blocks -> correct output schema (name, value expression).
- Expression evaluation with populated eval context — literals, variable refs, resource attribute refs, function calls.

**Clean preview test** (ultimate acceptance test):
- Translate TF state with modules -> Pulumi state with populated component inputs/outputs.
- Generate component code from parsed HCL.
- Run `pulumi preview` -> zero diffs.

## Backward Compatibility

- Default behavior changes: modules now produce component resources. Existing users migrating without modules are unaffected.
- `--no-module-components` flag preserves the current flat behavior for users who need it.
- Migration file format is backward compatible — `modules` field is optional.
