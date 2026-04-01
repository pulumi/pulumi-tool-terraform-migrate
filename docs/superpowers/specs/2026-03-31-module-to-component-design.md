# Terraform Modules to Pulumi Component Resources

**Date**: 2026-03-31
**Status**: Phase 1 implemented (PRs 1-4 on `feat/mc-*` branches), Phase 2 not started

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

Research into the Pulumi engine (`pulumi/pulumi`) revealed:

1. **Inputs ARE stored** — Whatever `args` are passed to the component constructor gets written to the `inputs` field of the resource state via the `RegisterResource` RPC.

2. **Outputs ARE stored** — Set by `registerOutputs()`. The Node.js SDK calls it automatically with `{}` if not called explicitly.

3. **Diff is `DeepEquals` on inputs only** — On `pulumi preview`, the engine compares old inputs vs new inputs using a simple `DeepEquals` (no provider involvement for components). If they don't match, a diff/update is shown. **Outputs are NOT compared during diff.**

4. **No provider involvement** — Components have no provider, so no `Check`, `Create`, `Diff`, or `Update` RPCs. The engine just stores what the program sends.

This means:
- For a **clean `pulumi preview`**, the translated state's component `inputs` must exactly match what the generated code will pass to the constructor.
- Component `outputs` don't cause diffs but should be populated for state fidelity.
- The chain is: TF state → translate (with component inputs/outputs) → Pulumi state → generate matching component code → `pulumi preview` = 0 diffs.

### Why HCL Parsing is Required

HCL parsing serves two purposes:

1. **Component interface extraction** (always needed) — `variable` blocks define constructor args, `output` blocks define public properties. This tells the code generator what the component class signature should look like. Only available in HCL source files.

2. **Input value resolution** (needed for clean preview) — Parse call site expressions and evaluate them against TF state + tfvars to get concrete values. Input values at call sites can be complex HCL expressions (`var.cidr`, `module.other.output`, `join(...)`, conditionals), requiring a full HCL expression evaluator with access to the Terraform function library.

**Output values can be sourced from raw state** — Since `opentofu/states.Module.OutputValues` contains resolved output values, we can read them directly from the raw `.tfstate` file instead of evaluating output expressions from HCL. This is simpler and more reliable than expression evaluation. The tool already has a `pkg/statefile/` package that reads raw state via the `opentofu/states` library.

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

#### Key Code Changes (Phase 2)

| File | Change |
|------|--------|
| `pkg/hcl/` (new package) | HCL parsing, module definition extraction, expression evaluation |
| `pkg/state_adapter.go` | Populate component inputs/outputs from parsed HCL |
| `pkg/migration/migration.go` | Add `HCLSource` field to `Module` struct |
| `cmd/stack.go` | Add `--module-source-map` flag |

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
