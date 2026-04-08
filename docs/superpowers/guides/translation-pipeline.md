# Terraform → Pulumi State Translation Pipeline

**Date**: 2026-04-05
**Branch**: `feat/mc-*` stack (mc-01 through mc-24)

This document describes the data flow through the `pulumi-tool-terraform-migrate` tool — how it reads a Terraform state file, translates every resource and module into Pulumi's format, and writes a Pulumi stack export that can be imported with `pulumi stack import`.

---

## Pipeline Overview

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         INPUT SOURCES                                   │
│                                                                         │
│  terraform.tfstate ──┐    *.tf HCL sources ──┐    terraform.tfvars ──┐  │
│  (TF state JSON v4)  │    (module definitions)│    (variable values)  │  │
│                      │                        │                       │  │
│  .terraform/         │    Pulumi project ─────┤                       │  │
│  (provider plugins,  │    (existing stack)     │                       │  │
│   module cache)      │                        │                       │  │
└──────────────────────┼────────────────────────┼───────────────────────┘  │
                       │                        │                          │
                       ▼                        ▼                          │
┌──────────────────────────────────────────────────────────────────────────┘
│
│  ┌─────────────────────────────────────────────────────────────────────┐
│  │                    1. LOAD TERRAFORM STATE                          │
│  │                                                                     │
│  │  tofu.LoadTerraformState()                                          │
│  │    ├── tofu init (initialize providers)                             │
│  │    └── tofu show -json → *tfjson.State                              │
│  │                                                                     │
│  │  tofu.GetProviderVersions()                                         │
│  │    └── tofu version -json → provider version map                    │
│  └─────────────────────────────────┬───────────────────────────────────┘
│                                    │
│                                    ▼ tfjson.State
│  ┌─────────────────────────────────────────────────────────────────────┐
│  │                    2. RESOLVE PULUMI PROVIDERS                      │
│  │                                                                     │
│  │  GetPulumiProvidersForTerraformState()                              │
│  │    For each TF provider (e.g., hashicorp/aws):                      │
│  │    ├── providermap.RecommendPulumiProvider()                        │
│  │    │     └── Checks static bridge list, falls back to dynamic       │
│  │    ├── If static: install binary, get resource mapping              │
│  │    └── If dynamic: bridge via GetMappingForTerraformProvider()      │
│  │                                                                     │
│  │  Output: map[TFProviderName]*ProviderWithMetadata                   │
│  │          (bridge info, resource schema, TF↔Pulumi name mappings)    │
│  └─────────────────────────────────┬───────────────────────────────────┘
│                                    │
│                                    ▼
│  ┌─────────────────────────────────────────────────────────────────────┐
│  │                    3. CONVERT RESOURCES                              │
│  │                                                                     │
│  │  convertState()                                                     │
│  │    │                                                                │
│  │    ├─── Create Pulumi provider resources                            │
│  │    │      └── One per TF provider, with UUID + version name         │
│  │    │                                                                │
│  │    └─── tofu.VisitResources() — walk TF state tree                  │
│  │           For each tfjson.StateResource:                             │
│  │                                                                     │
│  │           convertResourceStateExceptProviderLink()                   │
│  │           ┌──────────────────────────────────────────────────┐       │
│  │           │ a. Schema lookup                                 │       │
│  │           │    prov.P.ResourcesMap().Get(res.Type)           │       │
│  │           │                                                  │       │
│  │           │ b. Compute CTY type from TF schema               │       │
│  │           │    bridge.ImpliedType(schema)                    │       │
│  │           │                                                  │       │
│  │           │ c. Marshal TF JSON attrs → cty.Value             │       │
│  │           │    tofu.StateToCtyValue(res, ctyType)            │       │
│  │           │                                                  │       │
│  │           │ d. Extract sensitive paths                        │       │
│  │           │    res.SensitiveValues → []cty.Path              │       │
│  │           │                                                  │       │
│  │           │ e. Get Pulumi type token                         │       │
│  │           │    bridge.PulumiTypeToken(res.Type, prov)        │       │
│  │           │    e.g., "aws:s3/bucket:Bucket"                  │       │
│  │           │                                                  │       │
│  │           │ f. Convert value: CTY → Pulumi PropertyMap       │       │
│  │           │    tfbridge.MakeTerraformResult()                │       │
│  │           │    (snake_case → camelCase, type coercion)       │       │
│  │           │                                                  │       │
│  │           │ g. Mark secrets                                  │       │
│  │           │    ensureSecrets(props, sensitivePaths)           │       │
│  │           │    → resource.MakeSecret() for each path         │       │
│  │           │                                                  │       │
│  │           │ h. Separate inputs from outputs                  │       │
│  │           │    tfbridge.ExtractInputsFromOutputs()           │       │
│  │           └──────────────────────────────────────────────────┘       │
│  │                                                                     │
│  │  Output: PulumiState { Providers[], Resources[] }                   │
│  └─────────────────────────────────┬───────────────────────────────────┘
│                                    │
│                                    ▼
│  ┌─────────────────────────────────────────────────────────────────────┐
│  │              4. BUILD COMPONENT TREE (modules → components)         │
│  │              (when --no-module-components is NOT set)                │
│  │                                                                     │
│  │  buildComponentTree(resourceAddresses, typeOverrides)               │
│  │    ├── Parse module paths from TF resource addresses                │
│  │    │   "module.vpc.aws_subnet.this" → ["vpc"]                       │
│  │    │   "module.vpc.module.sub.aws_rt.rt" → ["vpc", "sub"]           │
│  │    ├── Build tree of componentNode (name, type token, children)     │
│  │    └── Apply type overrides from --module-type-map                  │
│  │                                                                     │
│  │  toComponents(tree) → []PulumiResource                              │
│  │    └── Flatten tree to component resources (custom=false)           │
│  │        with correct parent chain and type tokens                    │
│  │                                                                     │
│  │  Output: pulumiState.Components[]                                   │
│  └─────────────────────────────────┬───────────────────────────────────┘
│                                    │
│                                    ▼
│  ┌─────────────────────────────────────────────────────────────────────┐
│  │              5. POPULATE COMPONENT INPUTS & OUTPUTS                  │
│  │              (HCL parsing + expression evaluation)                   │
│  │                                                                     │
│  │  populateComponentsFromHCL()                                        │
│  │    │                                                                │
│  │    ├─── Parse HCL sources                                           │
│  │    │    ├── ParseModuleCallSites(tfSourceDir)                       │
│  │    │    │     → module "vpc" { source=..., cidr=var.x, ... }        │
│  │    │    ├── LoadAllTfvars(tfSourceDir)                               │
│  │    │    │     → terraform.tfvars + *.auto.tfvars values             │
│  │    │    ├── ResolveModuleSourcesFromCache(tfSourceDir)              │
│  │    │    │     → .terraform/modules/ path resolution                 │
│  │    │    └── ParseModuleVariables / ParseModuleOutputs per module    │
│  │    │                                                                │
│  │    ├─── Build evaluation context                                    │
│  │    │    ├── scopedAttrs.forModule("") → root resource attrs         │
│  │    │    ├── buildDataSourceAttrMap() → data.* namespace             │
│  │    │    ├── moduleOutputValues → module.* namespace (pre-pass)      │
│  │    │    └── Full TF function library (opentofu/lang.Scope)          │
│  │    │                                                                │
│  │    ├─── Pre-pass: evaluate module outputs for cross-references      │
│  │    │    Phase 1: leaf modules (no children)                         │
│  │    │      evaluatePrePassOutputs(node, sourcePath, scopedAttrs)     │
│  │    │    Phase 2: parent modules (with child outputs available)      │
│  │    │      buildChildModuleOutputs() + evaluatePrePassOutputs()      │
│  │    │                                                                │
│  │    ├─── Evaluate inputs (for each component)                        │
│  │    │    ├── Build EvalContext with var.*, resource.*, module.*,      │
│  │    │    │   data.*, path.*, local.*, count.*, each.*                 │
│  │    │    ├── EvaluateExpression(argExpr) for each call-site arg      │
│  │    │    │     e.g., cidr = var.vpc_cidr → "10.0.0.0/16"            │
│  │    │    │     e.g., name = join("-", [...]) → "prod-vpc"            │
│  │    │    └── Convert cty.Value → resource.PropertyMap                │
│  │    │                                                                │
│  │    └─── Evaluate outputs (for each component)                       │
│  │         ├── Build module-scoped EvalContext                         │
│  │         │   (child resource attrs, var.*, local.*, module.*)        │
│  │         ├── registerMissingResourceTypes() for count=0 resources    │
│  │         ├── evaluateAndAddLocals(sourcePath, evalCtx)               │
│  │         ├── EvaluateExpression(outputExpr) for each output          │
│  │         └── Convert to PropertyMap                                  │
│  │                                                                     │
│  │  Also writes: component-schemas.json (sidecar metadata)             │
│  │    → variable names, Pulumi types, defaults, output names           │
│  │    → consumed by code generation agent                              │
│  │                                                                     │
│  │  Output: Components[] with populated Inputs + Outputs               │
│  └─────────────────────────────────┬───────────────────────────────────┘
│                                    │
│                                    ▼
│  ┌─────────────────────────────────────────────────────────────────────┐
│  │              6. ASSEMBLE PULUMI DEPLOYMENT                          │
│  │                                                                     │
│  │  InsertResourcesIntoDeployment(pulumiState, stackName, projectName) │
│  │    │                                                                │
│  │    ├─── Validate Stack resource exists in deployment                │
│  │    │                                                                │
│  │    ├─── Insert provider resources                                   │
│  │    │    apitype.ResourceV3 { URN, Type, Inputs, Outputs }           │
│  │    │                                                                │
│  │    ├─── Insert component resources (depth-first order)              │
│  │    │    apitype.ResourceV3 { URN, Type, Custom=false,               │
│  │    │                         Inputs, Outputs, Parent }              │
│  │    │                                                                │
│  │    └─── Insert custom resources                                     │
│  │         apitype.ResourceV3 { URN, Type, Custom=true, ID,           │
│  │                              Inputs, Outputs, Provider, Parent }    │
│  │                                                                     │
│  │  Insertion order in deployment:                                      │
│  │    Stack → Providers → Components → Resources                       │
│  │                                                                     │
│  │  Output: apitype.DeploymentV3                                       │
│  └─────────────────────────────────┬───────────────────────────────────┘
│                                    │
│                                    ▼
│  ┌─────────────────────────────────────────────────────────────────────┐
│  │              7. WRITE OUTPUT                                        │
│  │                                                                     │
│  │  StackExport { Version: 3, Deployment: DeploymentV3 }               │
│  │    ├── json.Marshal() → pulumi-state.json                           │
│  │    └── json.Marshal() → required-providers.json (optional)          │
│  │                                                                     │
│  │  component-schemas.json (from step 5, always written)               │
│  │                                                                     │
│  │  Usage:                                                             │
│  │    pulumi stack import --file pulumi-state.json                     │
│  └─────────────────────────────────────────────────────────────────────┘
```

---

## Terraform State: Format, Storage, and Secrets

### State Format (JSON v4)

Terraform state uses a JSON format (format version `"1.0"`, state version 4). The file contains a complete snapshot of all managed infrastructure:

```json
{
  "format_version": "1.0",
  "terraform_version": "1.9.0",
  "values": {
    "root_module": {
      "resources": [
        {
          "address": "aws_vpc.main",
          "mode": "managed",
          "type": "aws_vpc",
          "name": "main",
          "provider_name": "registry.terraform.io/hashicorp/aws",
          "values": {
            "cidr_block": "10.0.0.0/16",
            "id": "vpc-0abc123",
            "tags": { "Name": "production" }
          },
          "sensitive_values": {
            "tags": {}
          }
        }
      ],
      "child_modules": [
        {
          "address": "module.vpc",
          "resources": [ ... ],
          "child_modules": [ ... ]
        }
      ]
    },
    "outputs": {
      "vpc_id": {
        "value": "vpc-0abc123",
        "sensitive": false
      }
    }
  }
}
```

### What's in TF State vs. What's Not

| In state | Not in state |
|----------|-------------|
| Resource attributes (all current values) | Module input variable values |
| Resource addresses and types | Module output values (JSON format) |
| Provider references | HCL source code or expressions |
| Root module outputs | Variable defaults or type constraints |
| Data source attributes | Local values |
| Module hierarchy (child_modules tree) | Plan/apply history |
| Sensitive value markers | Provider configuration details |

**Module inputs** are resolved at plan/apply time and baked into child resource attributes. They are not stored separately. This is why HCL parsing is required for component input population.

**Module outputs** are computed at runtime and not serialized in the JSON state format. The raw `.tfstate` binary format does store them in `Module.OutputValues`, but the JSON format produced by `tofu show -json` omits them. This tool evaluates output expressions from HCL source using child resource attributes from state as the eval context.

### Secrets: Yes, They Are in Plaintext

**Terraform state stores all values — including secrets — in plaintext.** There is no encryption layer in the state file format itself.

Sensitive values are tracked via a separate `sensitive_values` metadata object on each resource. This is a boolean map that mirrors the structure of `values` and marks which fields contain sensitive data:

```json
{
  "address": "random_password.db",
  "type": "random_password",
  "values": {
    "result": "7BDcazvBGyfvBW@p",
    "bcrypt_hash": "$2a$10$xYz...",
    "length": 16,
    "special": true
  },
  "sensitive_values": {
    "result": true,
    "bcrypt_hash": true
  }
}
```

The `sensitive_values` structure supports nested objects (`{"auth": {"token": true}}`), arrays (`[true, false, true]`), and deeply nested combinations. But it is **metadata only** — it does not redact or encrypt the corresponding values.

**Security implications:**
- Anyone with read access to the state file can see all secrets
- Terraform recommends encrypting state at rest via backend configuration (S3 with SSE, Azure Blob encryption, etc.)
- The `sensitive` attribute in HCL only controls CLI output masking and plan display — it does not encrypt state
- State locking (via DynamoDB, etc.) prevents concurrent writes but not reads

### How This Tool Handles Secrets

The translation pipeline preserves secret metadata through the conversion:

```
TF State                          Pulumi State
─────────────────                 ─────────────────
values.result = "secret123"   →   outputs.result = Secret("secret123")
sensitive_values.result = true     (wrapped in resource.Secret)
```

**Process:**
1. `res.SensitiveValues` is unmarshaled into `map[string]interface{}`
2. `tofu.SensitiveObjToCtyPath()` converts the boolean map to `[]cty.Path` — one path per sensitive leaf
3. After CTY → Pulumi value conversion, `ensureSecrets()` walks the paths and wraps each value with `resource.MakeSecret()`
4. Pulumi then encrypts these secret values using its secrets provider (passphrase, AWS KMS, etc.) when the state is stored

**Result:** Fields marked `sensitive` in Terraform become properly encrypted secrets in Pulumi state, gaining actual encryption rather than just metadata flags.

---

## Key Data Transformations

### Resource Value Journey

```
TF State JSON (map[string]interface{})
    │
    ▼  tofu.StateToCtyValue()
cty.Value (hashicorp type system)
    │
    ▼  tfbridge.MakeTerraformResult()
resource.PropertyMap (Pulumi type system)
    │  - snake_case keys → camelCase keys
    │  - TF types → Pulumi types
    │  - sensitive paths → resource.Secret wrappers
    │
    ▼  tfbridge.ExtractInputsFromOutputs()
Inputs: resource.PropertyMap (user-specified fields only)
Outputs: resource.PropertyMap (all fields including computed)
    │
    ▼  InsertResourcesIntoDeployment()
apitype.ResourceV3 { Inputs, Outputs }
    │
    ▼  json.Marshal()
Pulumi stack export JSON
```

### Component Value Journey

```
HCL Source Files
    │
    ├── ParseModuleCallSites()     → argument expressions
    ├── ParseModuleVariables()     → variable declarations
    ├── ParseModuleOutputs()       → output expressions
    ├── ParseLocals()              → local value definitions
    └── LoadAllTfvars()            → variable values
    │
    ▼  Build EvalContext (var.*, resource.*, module.*, data.*, local.*, path.*)
hcl.EvalContext + opentofu/lang function library
    │
    ▼  EvaluateExpression(expr)
cty.Value (concrete evaluated result)
    │
    ▼  CtyValueToPulumiPropertyValue()
resource.PropertyValue
    │
    ▼  Collect into PropertyMap
Component Inputs / Outputs
    │
    ▼  InsertResourcesIntoDeployment()
apitype.ResourceV3 { Custom: false, Inputs, Outputs, Parent }
```

### Module Hierarchy Translation

```
Terraform                              Pulumi
──────────────────                     ──────────────────
module.vpc                         →   vpc (ComponentResource)
  ├── aws_vpc.this                 →     this (aws:ec2/vpc:Vpc, parent=vpc)
  └── module.subnets               →     subnets (ComponentResource, parent=vpc)
        └── aws_subnet.this        →       this (aws:ec2/subnet:Subnet, parent=subnets)

module.buckets["logs"]             →   buckets-logs (ComponentResource)
  └── aws_s3_bucket.this           →     this (aws:s3/bucket:Bucket, parent=buckets-logs)

module.buckets["data"]             →   buckets-data (ComponentResource)
  └── aws_s3_bucket.this           →     this (aws:s3/bucket:Bucket, parent=buckets-data)
```

---

## File Map

| File | Role |
|------|------|
| `cmd/stack.go` | CLI entry point, flag parsing |
| `pkg/state_adapter.go` | Main orchestrator: `TranslateAndWriteState()`, `TranslateState()`, `convertState()` |
| `pkg/tofu/loader.go` | Load TF state via OpenTofu subprocess |
| `pkg/tofu/visitors.go` | `VisitResources()` — recursive TF state tree traversal |
| `pkg/tofu/state_to_cty.go` | `StateToCtyValue()` — JSON attrs to cty.Value |
| `pkg/tofu/sensitive_obj_to_cty_path.go` | `SensitiveObjToCtyPath()` — sensitive marker parsing |
| `pkg/pulumi_providers.go` | `GetPulumiProvidersForTerraformState()` — provider resolution |
| `pkg/providermap/` | Provider recommendation engine (static + dynamic bridges) |
| `pkg/bridge/implied_type.go` | `ImpliedType()` — TF schema to cty.Type |
| `pkg/bridge/pulumi_type_token.go` | `PulumiTypeToken()` — TF type to Pulumi type token |
| `pkg/convert_tf_value_to_pulumi.go` | `ConvertTFValueToPulumiValue()`, `ensureSecrets()` |
| `pkg/component_populate.go` | `populateComponentsFromHCL()` — HCL eval for component I/O |
| `pkg/hcl/evaluator.go` | `EvalContext`, `EvaluateExpression()` — HCL expression evaluator |
| `pkg/hcl/parser.go` | Module call site, variable, output, locals parsing |
| `pkg/component_metadata.go` | `ComponentSchemaMetadata` — sidecar schema generation |
| `pkg/pulumi_state.go` | `InsertResourcesIntoDeployment()`, `makeUrn()` |
