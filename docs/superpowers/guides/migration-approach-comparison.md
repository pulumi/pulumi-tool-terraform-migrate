# TF → Pulumi Migration: Approach Comparison

## Summary

Migrating Terraform workspaces to Pulumi means transferring resource ownership — not recreating infrastructure. Pulumi must end up with state containing the same physical resource IDs as Terraform, so that `pulumi preview` shows no diff.

There are two migration approaches:

- **Code-first:** Agent reads TF source → generates Pulumi code → imports resource IDs from cloud → iterates until preview is clean. Simpler tooling, but the agent works blind — it has no visibility into actual resource values or component shapes.

- **State-first (recommended):** Run `pulumi-terraform-migrate` → import the full translated state → agent generates code to match known state. The agent has maximum context: exact inputs, outputs, component hierarchy, and provider versions. Fewer iteration cycles.

For OSS modules (e.g., `terraform-aws-modules/vpc`), the recommended strategy is generating a **targeted Pulumi component** scoped to only the features actually used — informed by the translated state. This avoids both the maintenance burden of a full rewrite and the limitations of the `pulumi-terraform-module` plugin.

Across subsequent workspace migrations, component types are reused via `--module-type-map` and validated via `--module-schema`, so only the first workspace requires building the component.

---

## 1. How Pulumi Tracks Infrastructure

Pulumi identifies every managed resource using two coordinates:

- **URN (logical identity):** A structured string encoding the stack, project, type hierarchy, and resource name.
- **ID (physical identity):** The cloud provider's unique identifier (e.g., `eipalloc-0ce452cc754bf50af`).

### URN Format

```
urn:pulumi:<stack>::<project>::<parent-type-chain$type>::<name>
```

When nested inside a component, the parent's type chain appears before a `$` delimiter:

```
urn:pulumi:dev::dns-to-db::terraform:module/vpc:Vpc$aws:ec2/subnet:Subnet::vpc-public-us-east-1a
```

### The Provider `Read(id)` Operation

The provider's `Read` method fetches a resource's current properties from the cloud using its physical ID. It is called during:

- **`pulumi import`** — to populate state when importing a resource by cloud ID.
- **`pulumi refresh`** — to reconcile state with cloud reality.

During `pulumi preview`/`pulumi up`, Pulumi does **not** call `Read`. It diffs the code's inputs against the inputs stored in state.

### State = Record of Ownership

Pulumi state records what Pulumi believes it owns. Every custom resource entry contains:

| Field | Purpose |
|-------|---------|
| `urn` | Logical identity (stack + project + type chain + name) |
| `id` | Physical cloud resource ID |
| `type` | Pulumi resource type token |
| `inputs` | The arguments passed to create/update the resource |
| `outputs` | The full properties returned by the provider after creation |
| `provider` | Reference to the provider instance (URN + ID) |
| `parent` | URN of the parent resource (stack root or component) |

See [Appendix A](#appendix-a--full-resource-state-example) for a complete example with all fields.

---

## 2. What Happens During a TF → Pulumi Migration

The goal is to **transfer resource ownership** from Terraform to Pulumi without touching infrastructure. Physical resources remain unchanged — only the control plane record changes.

### The Key Requirement

Pulumi state must contain the same physical resource IDs as TF state. After migration:

```
Infrastructure (cloud) = Pulumi state = Pulumi code
```

A successful migration produces `pulumi preview` with **no diff**.

### How State Import Works

`pulumi stack import --file state.json` loads a state file as-is into Pulumi's backend. No transformation, no code validation. The state simply becomes Pulumi's record of what exists.

### How Preview Reconciles State and Code

When you run `pulumi preview`, the engine:

1. **Walks your code** — executes the Pulumi program, registering resources.
2. **Computes URNs** — each resource registration produces a URN from the type + name + parent chain.
3. **Matches URNs to state** — looks up each computed URN in the imported state.
4. **Diffs inputs** — for matched resources, compares the code's inputs against the inputs stored in state (no cloud call).

| Scenario | Preview Shows | Meaning |
|----------|--------------|---------|
| URN in both code and state | **Update** (if code inputs differ from state inputs) or **no change** | Resource is managed and code matches |
| URN in state only | **Delete** | State has a resource that code doesn't declare |
| URN in code only | **Create** | Code declares a resource not in state |

The iteration loop after import: adjust code until all URNs match and all input diffs resolve.

---

## 3. Approach A — Code-First

The agent works from Terraform source code to produce Pulumi code, then imports cloud resource IDs to populate state.

### Workflow

1. Agent reads TF module source → generates Pulumi component code
2. Agent reads TF root module → generates Pulumi program
3. Generate a minimal import JSON file (type + name + cloud ID per resource)
4. `pulumi import --file import.json` — Pulumi calls `Read(id)` for each resource, populates state from cloud
5. `pulumi preview` — diff code inputs vs state inputs, iterate until no diff

### Import JSON vs Translated State

For an Elastic IP, the import file contains only three fields:

```json
{
  "resources": [
    {
      "type": "aws:ec2/eip:Eip",
      "name": "bastion_eip",
      "id": "eipalloc-0ce452cc754bf50af"
    }
  ]
}
```

Compare this to the full translated state entry in [Appendix A](#appendix-a--full-resource-state-example) — no inputs, no outputs, no provider link, no parent. State gets populated entirely from the cloud `Read` call.

### Pros

- Simpler tooling — no state translation tool needed
- Agent works from familiar TF code patterns

### Cons

- **Agent must infer component interface shape** from TF module source (which variables matter, types, etc.)
- **No component hierarchy in imported state** — `pulumi import` produces flat resources only
- **Agent has no visibility into actual resource property values** — works blind from TF code
- **More iteration cycles:** agent guesses at inputs, sees diff, adjusts
- **Import JSON has no inputs/outputs** — state gets populated purely from cloud `Read`, which may differ from what the TF code configured

---

## 4. Approach B — State-First (Recommended)

`pulumi-terraform-migrate` translates the full Terraform state into a Pulumi state file with inputs, outputs, component hierarchy, and provider versions. The agent generates code to match this known state.

### Workflow

1. Run `pulumi-terraform-migrate stack` → produces full translated state + `component-schemas.json` + plugin versions
2. `pulumi stack import --file pulumi-state.json` — full state with inputs, outputs, components, providers
3. Agent reads translated state + `component-schemas.json` → generates code with precise knowledge of:
   - Exact inputs each component receives (call-site arguments only)
   - Exact outputs each component produces
   - Full resource property values (what was actually deployed)
   - Component hierarchy (parent-child nesting matching TF module structure)
   - Provider versions
4. `pulumi preview` — compare code vs state, iterate until no diff

### What the Agent Sees: Component in Translated State

The ACM module from DNS-to-DB produces this component entry:

```json
{
  "urn": "urn:pulumi:dev::dns-to-db::terraform:module/acm:Acm::acm",
  "custom": false,
  "type": "terraform:module/acm:Acm",
  "inputs": {
    "domain_name": "pulumi-demos.net",
    "subject_alternative_names": ["*.pulumi-demos.net"],
    "tags": {
      "Owner": "anton",
      "environment": "dev",
      "owners": "sap"
    },
    "validation_method": "DNS",
    "wait_for_validation": true,
    "zone_id": "Z02930341VU8RB820DVY1"
  },
  "outputs": {
    "acm_certificate_arn": "",
    "acm_certificate_domain_validation_options": [
      {
        "domain_name": "*.pulumi-demos.net",
        "resource_record_name": "_738dd0976bcebcf09c7f1699245390a4.pulumi-demos.net.",
        "resource_record_type": "CNAME",
        "resource_record_value": "_5bc0e1ecea22d56eafbd5b3bdf2be98b.jfrzftwwjs.acm-validations.aws."
      }
    ],
    "acm_certificate_status": "",
    "acm_certificate_validation_emails": [],
    "distinct_domain_names": ["pulumi-demos.net"],
    "validation_route53_record_fqdns": [
      "_738dd0976bcebcf09c7f1699245390a4.pulumi-demos.net"
    ]
  },
  "parent": "urn:pulumi:dev::dns-to-db::pulumi:pulumi:Stack::dns-to-db-dev"
}
```

- `custom: false` — component (logical grouping), not a cloud resource
- `inputs` contains only the 6 arguments passed at the call site, not all 20 variables the ACM module declares
- `outputs` contains the module's output values
- No `id` or `provider` — components don't have physical cloud identities

### Pros

- **Agent has maximum context:** actual property values, component shapes, exact inputs
- **Component hierarchy already in state** — code just needs to match it
- **Fewer iteration cycles:** agent knows exactly what inputs/outputs to declare
- **`component-schemas.json` gives typed interfaces** (names, Pulumi types, defaults, descriptions)
- **State is 1:1 with TF state** — all resource IDs preserved
- **Subsequent workspace migrations reuse component types** via `--module-type-map`

### Cons

- Requires running the translation tool (extra step in pipeline)

### Why State-First Is Preferable

The agent's accuracy is proportional to the specificity of its context.

- **Code-first:** agent infers what should exist → generates code → discovers mismatches at preview.
- **State-first:** agent knows exactly what exists → generates matching code → fewer mismatches.

The table in Section 5 quantifies the difference: for the DNS-to-DB fixture, the translated state reduces the input surface by 67–92% across large modules. The agent doesn't need to reason about hundreds of unused variables.

---

## 5. OSS Module Strategies (Worst to Best)

When migrating workspaces that use OSS Terraform modules (e.g., `terraform-aws-modules/vpc`), there are three options for the Pulumi equivalent.

### Option 1: Full Pulumi Component Rewrite of OSS Module (Least Recommended)

Generate a complete Pulumi component that replicates the entire OSS module's feature surface.

**Downside:** Large OSS modules expose hundreds of variables covering every possible configuration. The customer likely uses a fraction of these features. Rewriting the full module is unnecessary work and creates a large maintenance surface.

**When it makes sense:** Almost never — only if the customer plans to use the component as a shared library across many teams with diverse needs.

### Option 2: `pulumi-terraform-module` Plugin (Intermediate)

Use the `terraform-module` provider to run the original TF module as-is within Pulumi.

**How it works:** Pulumi installs and manages OpenTofu behind the scenes, translates Pulumi resource declarations into TF config, executes the module, and stores state in Pulumi's backend with encryption.

**Pros:**
- Zero rewrite effort
- Full access to 16,000+ TF registry modules
- Good for gradual migration

**Cons/Limitations:**
- Experimental — may have breaking changes
- No support for Pulumi `transforms` resource option
- Cannot use targeted updates (`pulumi up --target`)
- Individual resources within the module cannot be `protect`ed
- Modules execute in isolated temp directories — requires absolute file paths
- Weaker type inference than native Pulumi (TF modules lack metadata for Pulumi to always infer correct output types)
- Depends on OpenTofu runtime — adds a runtime dependency

**When it makes sense:** As a bridge during migration. Gets the customer onto Pulumi quickly. Can be replaced with native components later when time allows.

### Option 3: Targeted Pulumi Component with Only Used Features (Recommended)

Generate a smaller Pulumi component that includes only the features the customer actually uses, informed by the translated state.

**How it works:** The translated state shows exactly which inputs were passed at the call site. `component-schemas.json` provides the typed interface. The agent generates a component that handles only the used inputs + the resources they create.

**Pros:**
- Component is under the customer's control (no external dependency)
- Smaller, more understandable code — scoped to actual usage (see table below)
- Full Pulumi lifecycle control (transforms, targeting, protect all work)
- No OpenTofu runtime dependency
- Idiomatic Pulumi — uses native SDK constructs

**Cons:**
- More upfront work than Option 2 (but much less than Option 1)
- Customer owns maintenance (but scope is limited)

**Why state-first makes this viable:** Without translated state, the agent would need to analyze the full OSS module source to determine which features are actually used. With translated state, the answer is already there — `component-schemas.json` shows the exact interface, and the state shows the exact resource property values.

### DNS-to-DB Module Summary

All 15 modules with their total variable count (from `component-schemas.json`) vs actual call-site inputs (from translated state):

| Module | Total Variables | Call-Site Inputs | Reduction |
|--------|:-:|:-:|:-:|
| `module.vpc` | 222 | 17 | 92% |
| `module.rdsdb` | 90 | 30 | 67% |
| `module.ec2_private_app1` | 80 | 8 | 90% |
| `module.ec2_private_app2` | 80 | 8 | 90% |
| `module.ec2_private_app3` | 80 | 7 | 91% |
| `module.ec2_public` | 80 | 8 | 90% |
| `module.loadbalancer_sg` | 56 | 8 | 86% |
| `module.private_sg` | 56 | 7 | 88% |
| `module.public_bastion_sg` | 56 | 7 | 88% |
| `module.rdsdb_sg` | 56 | 6 | 89% |
| `module.alb` | 43 | 9 | 79% |
| `module.acm` | 20 | 6 | 70% |
| `module.db_option_group` | 9 | 9 | 0% |
| `module.db_parameter_group` | 7 | 7 | 0% |
| `module.db_subnet_group` | 6 | 6 | 0% |

---

## 6. Component Providers and Schema-Driven Reuse

To benefit from `--module-schema` validation across workspace migrations, the generated Pulumi components should be built as **component providers** (multi-language components) — standalone plugin binaries (`pulumi-resource-{name}`) that expose components via gRPC.

| Aspect | ComponentResource (single-language) | Component Provider (MLC) |
|--------|-------------------------------------|--------------------------|
| Consumable from | One language only | Any Pulumi language |
| Schema | None (no formal interface) | Full Pulumi package schema |
| Distribution | Copy/paste or package manager | `pulumi plugin install` or `pulumi package add` |
| Validation | Runtime only | Schema-based at translation time |

See [Appendix B](#appendix-b--component-provider-schema-generation) for details on how schemas are generated from component code.

### Using `--module-type-map` for Subsequent Workspaces

After the component provider exists, subsequent workspace migrations map TF modules to the provider's component types:

```bash
# First workspace: auto-derives terraform:module/vpc:Vpc, agent generates component provider
pulumi-terraform-migrate stack --from ./workspace-1 --to ./pulumi-1 --out state.json

# Subsequent workspaces: map to the component provider's type
pulumi-terraform-migrate stack --from ./workspace-2 --to ./pulumi-2 --out state.json \
  --module-type-map module.vpc=myinfra:network:Vpc \
  --module-type-map module.rds=myinfra:database:Rds
```

The translated state for workspace-2 uses `myinfra:network:Vpc` as the component type token. The code generator imports the existing component provider package rather than creating a new component.

### Using `--module-schema` for Validation

The schema JSON extracted from the component provider binary is what `--module-schema` validates against:

```bash
# Extract schema from the component provider binary
pulumi package get-schema ./bin/pulumi-resource-myinfra > myinfra-schema.json

# Validate workspace-3's module usage against the existing component interface
pulumi-terraform-migrate stack --from ./workspace-3 --to ./pulumi-3 --out state.json \
  --module-schema module.vpc=./myinfra-schema.json
```

If workspace-3 passes inputs to the VPC module that aren't in the component provider's schema, the tool produces errors — surfacing integration issues before code generation begins.

### Multi-Workspace Migration Flow

```
Workspace 1                    Workspace 2+                   Schema Expansion
───────────                    ────────────                   ────────────────
translate state                translate state                workspace-N uses
  ↓                              + --module-type-map            new module features
agent generates                  ↓                              ↓
  targeted component           validate with                  schema validation
  provider (Option 3)            --module-schema                catches it
  ↓                              ↓                              ↓
build provider binary          generate only root             expand component
  ↓                              program (components            provider interface
extract schema                   already exist)                 ↓
  ↓                              ↓                            rebuild + re-extract
validate                       import                           schema
  ↓
import
```

1. **Workspace 1:** Translate state → agent generates targeted component provider (Option 3) → build provider binary → extract schema → validate → import
2. **Workspace 2+:** Translate state with `--module-type-map` pointing to component provider types → validate with `--module-schema` against extracted schema → generate only the root program (components already exist) → import
3. **Schema expansion:** If workspace-N uses module features not in the existing component provider, schema validation catches it. Expand the component provider's interface, rebuild, re-extract schema.

---

## Appendix A — Full Resource State Example

Complete translated state entry for an AWS Elastic IP from the DNS-to-DB fixture, produced by `pulumi-terraform-migrate stack`:

```json
{
  "urn": "urn:pulumi:dev::dns-to-db::aws:ec2/eip:Eip::bastion_eip",
  "custom": true,
  "id": "eipalloc-0ce452cc754bf50af",
  "type": "aws:ec2/eip:Eip",
  "inputs": {
    "__defaults": [],
    "domain": "vpc",
    "instance": "i-0f63614ea8457a865",
    "networkBorderGroup": "us-east-1",
    "networkInterface": "eni-07504d6223ea8f63e",
    "publicIpv4Pool": "amazon",
    "region": "us-east-1",
    "tags": {
      "Owner": "anton",
      "__defaults": [],
      "environment": "dev",
      "owners": "sap"
    },
    "tagsAll": {
      "Owner": "anton",
      "__defaults": [],
      "environment": "dev",
      "owners": "sap"
    }
  },
  "outputs": {
    "address": null,
    "allocationId": "eipalloc-0ce452cc754bf50af",
    "arn": "arn:aws:ec2:us-east-1:616138583583:elastic-ip/eipalloc-0ce452cc754bf50af",
    "associateWithPrivateIp": null,
    "associationId": "eipassoc-04f08b9412a939d71",
    "carrierIp": "",
    "customerOwnedIp": "",
    "customerOwnedIpv4Pool": "",
    "domain": "vpc",
    "id": "eipalloc-0ce452cc754bf50af",
    "instance": "i-0f63614ea8457a865",
    "ipamPoolId": null,
    "networkBorderGroup": "us-east-1",
    "networkInterface": "eni-07504d6223ea8f63e",
    "privateDns": "ip-10-0-101-39.ec2.internal",
    "privateIp": "10.0.101.39",
    "ptrRecord": "",
    "publicDns": "ec2-54-224-163-70.compute-1.amazonaws.com",
    "publicIp": "54.224.163.70",
    "publicIpv4Pool": "amazon",
    "region": "us-east-1",
    "tags": {
      "Owner": "anton",
      "environment": "dev",
      "owners": "sap"
    },
    "tagsAll": {
      "Owner": "anton",
      "environment": "dev",
      "owners": "sap"
    }
  },
  "parent": "urn:pulumi:dev::dns-to-db::pulumi:pulumi:Stack::dns-to-db-dev",
  "provider": "urn:pulumi:dev::dns-to-db::pulumi:providers:aws::default_7_23_0::0f4a5003-bd3c-4e4b-9d1f-3ef554bb2186"
}
```

**Key details:**

- `outputs` includes computed properties (`publicIp`, `privateDns`, `arn`) that only exist after the resource is created.
- `provider` links to a specific provider instance with its own URN and ID, pinning the provider version.
- `inputs.__defaults` is explained in [Appendix C](#appendix-c--the-__defaults-field).

---

## Appendix B — Component Provider Schema Generation

The schema is **code-first** — derived from the component's type definitions, not manually written:

1. **Author writes component** in Go/Python/TypeScript using the Pulumi provider SDK
   - Go example: struct fields with `pulumi` tags define inputs/outputs
   - The `Annotate()` method adds descriptions, defaults, and constraints
2. **Compile to binary** named `pulumi-resource-myinfra`
3. **Binary exposes `GetSchema` gRPC method** — Pulumi CLI calls this to extract the schema
4. **Extract schema JSON:** `pulumi package get-schema ./bin/pulumi-resource-myinfra` → outputs Pulumi package schema JSON
5. **Schema describes** each component's `inputProperties`, `requiredInputs`, `properties` (outputs), types

Example schema for a targeted VPC component:

```json
{
  "resources": {
    "myinfra:network:Vpc": {
      "isComponent": true,
      "inputProperties": {
        "name": { "type": "string" },
        "cidr": { "type": "string" },
        "azs": { "type": "array", "items": { "type": "string" } },
        "publicSubnets": { "type": "array", "items": { "type": "string" } },
        "privateSubnets": { "type": "array", "items": { "type": "string" } }
      },
      "requiredInputs": ["name", "cidr", "azs"]
    }
  }
}
```

---

## Appendix C — The `__defaults` Field

Every custom resource's `inputs` (and nested objects within inputs) contains a `__defaults` array. This is a Pulumi engine convention injected by the Terraform Bridge layer (`tfbridge.ExtractInputsFromOutputs`). It lists the names of input properties whose values came from **provider defaults** rather than explicit user configuration.

In translated state, `__defaults` is always an empty list `[]`. The tool reverse-engineers inputs from Terraform state outputs — it doesn't go through the normal `Create` flow where the provider would apply and track defaults. The empty list is the correct conservative choice: it tells Pulumi "treat all these inputs as explicitly set." This prevents the engine from trying to re-apply provider defaults on the first `pulumi up`, which would cause spurious diffs.

The field is **required** — if `__defaults` is missing, the Pulumi diff engine may misinterpret which values were explicit vs defaulted. It appears at every nesting level (top-level inputs, inside `tags`, inside nested blocks).

Note: `__defaults` applies only to **custom resources** (cloud resources translated via tfbridge). Component resources in the translated state do not have `__defaults` — their inputs come directly from HCL call-site evaluation, not from provider schema translation.
