# Migrating Terraform to Pulumi: A Practical Guide

This guide walks through the end-to-end process of migrating Terraform workspaces to Pulumi — from choosing which workspaces to migrate first, through the translation process, to operating in a mixed Terraform/Pulumi environment during the transition.

For a comparison of migration approaches (state-first vs. code-first) and strategies for handling OSS modules, see [Migration Approach Comparison](./migration-approach-comparison.md).

---

## Table of Contents

- [How Terraform State Works](#how-terraform-state-works)
- [How the Translation Works](#how-the-translation-works)
- [Planning a Migration](#planning-a-migration)
- [Per-Workspace Migration Steps](#per-workspace-migration-steps)
  - [Remote and OSS Modules](#remote-and-oss-modules)
- [Operating During Migration](#operating-during-migration)
- [Completing the Migration](#completing-the-migration)

---

## How Terraform State Works

Before migrating, it helps to understand what Terraform state contains and how it differs from Pulumi state — since the translation tool bridges the gap between the two.

### What Terraform State Is

Terraform state is a JSON file (format version `"1.0"`, state version 4) that records every resource Terraform manages. Each resource entry contains:

- **Address** — the Terraform identifier (e.g., `module.vpc.aws_subnet.public[0]`)
- **Type and provider** — the resource type and which provider manages it
- **Attribute values** — every property of the resource as it exists in the cloud
- **Sensitive value markers** — a boolean map flagging which attributes are sensitive

The state also records the module hierarchy (`child_modules` tree), provider references, and root module outputs. See HashiCorp's [state documentation](https://developer.hashicorp.com/terraform/language/state) for the full specification.

### What Terraform State Does NOT Contain

| Present in state | Absent from state |
|-----------------|-------------------|
| Resource attributes (current cloud values) | Module input variable values |
| Resource addresses and types | Module output values (in JSON format) |
| Provider references and versions | HCL expressions or source code |
| Root module outputs | Variable defaults or type constraints |
| Data source attributes | Local value definitions |
| Module hierarchy (child_modules) | Provider configuration |

The two most important gaps for migration:

1. **Module inputs** are resolved at `plan`/`apply` time and baked into child resource attributes. They are not stored separately. The translation tool recovers them by parsing HCL call-site expressions.

2. **Module outputs** (in JSON format) are computed at runtime and not serialized. The tool evaluates output `value` expressions from HCL source using child resource attributes from state.

### Secrets Are Stored in Plaintext

**Terraform state stores all values — including secrets — in plaintext.** The `sensitive_values` object is metadata only; it marks which fields are sensitive but does not encrypt them:

```json
{
  "values": {
    "result": "7BDcazvBGyfvBW@p",
    "bcrypt_hash": "$2a$10$xYz..."
  },
  "sensitive_values": {
    "result": true,
    "bcrypt_hash": true
  }
}
```

Anyone with read access to the state file can see all secrets. Terraform relies on backend-level encryption (S3 SSE, Azure Blob encryption, GCS CMEK) for protection. The `sensitive` attribute in HCL only controls CLI output masking — it does not encrypt state. See HashiCorp's [sensitive data in state](https://developer.hashicorp.com/terraform/language/state/sensitive-data) documentation.

**During migration, the translation tool preserves sensitivity metadata.** Fields marked sensitive in Terraform are wrapped with Pulumi's secret type, which is then encrypted by your [Pulumi secrets provider](https://www.pulumi.com/docs/iac/concepts/secrets/#available-encryption-providers) (passphrase, AWS KMS, Azure Key Vault, etc.). This is a security improvement — secrets gain actual encryption rather than just boolean flags.

---

## How the Translation Works

The translation tool reads your Terraform state file, resolves provider mappings, converts every resource into Pulumi's format, builds component resources from the module hierarchy, and writes a Pulumi stack export file.

### Translation Pipeline

```
                    INPUT
                    ─────
    terraform.tfstate    *.tf sources    terraform.tfvars
    .terraform/          Pulumi project
          │                    │               │
          ▼                    ▼               ▼
┌─────────────────────────────────────────────────────┐
│  1. Load TF State                                   │
│     Read state via OpenTofu, extract provider        │
│     versions                                         │
└───────────────────────┬─────────────────────────────┘
                        ▼
┌─────────────────────────────────────────────────────┐
│  2. Resolve Providers                               │
│     For each TF provider (aws, gcp, azure...),      │
│     find the matching Pulumi provider — either a     │
│     statically bridged native provider or a          │
│     dynamically bridged one                          │
└───────────────────────┬─────────────────────────────┘
                        ▼
┌─────────────────────────────────────────────────────┐
│  3. Convert Resources                               │
│     For each TF resource:                            │
│       • Map TF type → Pulumi type token              │
│         (aws_vpc → aws:ec2/vpc:Vpc)                  │
│       • Convert attribute values                     │
│         (snake_case → camelCase, type coercion)      │
│       • Preserve sensitive fields as Pulumi secrets  │
│       • Separate inputs from computed outputs        │
└───────────────────────┬─────────────────────────────┘
                        ▼
┌─────────────────────────────────────────────────────┐
│  4. Build Component Hierarchy                       │
│     Map TF module nesting → Pulumi ComponentResource │
│     tree with correct parent chains and type tokens  │
│                                                      │
│     module.vpc.aws_subnet.this                       │
│       → vpc (Component) → this (Subnet, parent=vpc) │
└───────────────────────┬─────────────────────────────┘
                        ▼
┌─────────────────────────────────────────────────────┐
│  5. Populate Component Inputs & Outputs             │
│     Parse HCL sources to recover module interface:   │
│       • Call-site arguments → component inputs       │
│       • Output value expressions → component outputs │
│       • Variable declarations → component-schemas    │
│     Evaluate HCL expressions using TF state data,   │
│     tfvars, locals, and the full Terraform function  │
│     library                                          │
└───────────────────────┬─────────────────────────────┘
                        ▼
┌─────────────────────────────────────────────────────┐
│  6. Assemble & Write                                │
│     Insert into Pulumi deployment:                   │
│       Stack → Providers → Components → Resources     │
│     Write pulumi-state.json                          │
│     Write component-schemas.json (sidecar metadata)  │
│     Write required-providers.json (plugin versions)  │
└─────────────────────────────────────────────────────┘

                    OUTPUT
                    ──────
    pulumi-state.json           → pulumi stack import --file
    component-schemas.json      → code generation agent input
    required-providers.json     → pulumi plugin install
```

### What Gets Translated

| Terraform concept | Pulumi equivalent |
|-------------------|-------------------|
| Resource (e.g., `aws_vpc.main`) | Custom resource (`aws:ec2/vpc:Vpc`) |
| Module (e.g., `module.vpc`) | [ComponentResource](https://www.pulumi.com/docs/iac/concepts/components/) |
| Module nesting | Parent-child component chain |
| Provider | [Provider resource](https://www.pulumi.com/docs/iac/concepts/providers/) |
| `count` / `for_each` on modules | Separate component instances (`vpc-0`, `vpc-us-east-1`) |
| Sensitive attributes | [Pulumi secrets](https://www.pulumi.com/docs/iac/concepts/secrets/) (encrypted) |
| Resource attributes | Inputs (user-specified) + Outputs (all including computed) |

### What the Translation Produces

**`pulumi-state.json`** — A complete [Pulumi stack export](https://www.pulumi.com/docs/iac/cli/commands/pulumi_stack_export/) containing every resource, component, and provider with full inputs, outputs, and parent relationships. Import with `pulumi stack import --file pulumi-state.json`.

**`component-schemas.json`** — A sidecar metadata file describing each component's typed interface: input variable names, Pulumi types, defaults, and output names. Used by the code generation agent to produce type-correct component code. Always written when HCL sources are available.

**`required-providers.json`** — A list of Pulumi provider plugins and versions needed by the translated state. Install with `pulumi plugin install`.

---

## Planning a Migration

### Start with Leaf Workspaces

The most important principle: **migrate leaf workspaces first** — application stacks that consume outputs from upstream infrastructure stacks. Work from the edges of your dependency graph inward toward foundational stacks.

```
                    Dependency Graph
                    ────────────────

           ┌──────────────┐
           │  networking   │  ← Migrate LAST (foundational)
           │  (VPC, DNS)   │
           └──────┬───────┘
                  │ outputs: vpc_id, subnet_ids, zone_id
                  │
        ┌─────────┼─────────┐
        ▼         ▼         ▼
   ┌─────────┐ ┌─────────┐ ┌─────────┐
   │  app-1  │ │  app-2  │ │  app-3  │  ← Migrate FIRST (leaf nodes)
   │ (EC2,   │ │ (ECS,   │ │ (Lambda │
   │  ALB)   │ │  RDS)   │ │  API GW)│
   └─────────┘ └─────────┘ └─────────┘
```

**Why leaf-first:**

- **No downstream dependents.** Migrating a leaf workspace doesn't affect any other workspace. If something goes wrong, the blast radius is limited.
- **Data flows one way.** Leaf workspaces consume upstream Terraform outputs but nothing consumes their state. You can read data from Terraform state into Pulumi programs (see [Cross-Workspace Data Flow](#cross-workspace-data-flow)), but you never need to push data from Pulumi back to Terraform.
- **Build confidence incrementally.** Each migrated leaf workspace validates the process and builds your Pulumi component library before you touch foundational infrastructure.
- **Simpler expressions.** Application stacks tend to have simpler module call-site expressions than networking stacks, which often have complex CIDR calculations and dynamic resource creation.

### Cross-Workspace Data Flow

During migration, your migrated Pulumi stacks will still need values from Terraform workspaces that haven't been migrated yet (e.g., VPC IDs, subnet IDs, DNS zone IDs from a networking stack). **Data should always flow from Terraform into Pulumi, never the reverse.**

There are several ways to pull Terraform outputs into Pulumi programs:

**Option 1: Read Terraform state directly.** Pulumi programs can read Terraform state files or remote backends using the [`terraform` package](https://www.pulumi.com/registry/packages/terraform/). This is the Pulumi equivalent of `terraform_remote_state`:

```typescript
import * as terraform from "@pulumi/terraform";

const networkState = new terraform.state.RemoteStateReference("network", {
    backendType: "s3",
    bucket: "my-terraform-state",
    key: "networking/terraform.tfstate",
    region: "us-east-1",
});

const vpcId = networkState.getOutput("vpc_id");
const subnetIds = networkState.getOutput("private_subnet_ids");
```

**Option 2: Use Pulumi ESC.** Store shared infrastructure values in [Pulumi ESC](https://www.pulumi.com/docs/esc/) (Environments, Secrets, and Configuration) environments. Write the values once (from Terraform outputs or manually), then reference them from any Pulumi stack:

```yaml
# ESC environment: networking/dev
values:
  vpc_id: vpc-0abc123
  subnet_ids:
    - subnet-aaa
    - subnet-bbb
  zone_id: Z02930341VU8RB820DVY1
```

```typescript
// Pulumi program reads from ESC via stack config
const config = new pulumi.Config();
const vpcId = config.require("vpcId");
```

ESC is the preferred approach when you want a clean abstraction layer that doesn't couple Pulumi programs to Terraform state file locations.

### Choosing Workspace Order Within Leaf Nodes

Among your leaf workspaces, prefer:

- **Non-production first** — Dev/staging environments let you validate the migration process before touching production
- **Common providers** — AWS, Azure, and GCP have mature Pulumi providers with complete resource mappings
- **Fewer/simpler modules** — Workspaces with flat resource layouts or simple local modules translate more cleanly than those with deeply nested module hierarchies or complex HCL expressions
- **Local modules only** — Workspaces using only `source = "./modules/..."` are simpler than those pulling from the Terraform Registry

**Defer more complex leaf workspaces:**

- **Heavy use of OSS registry modules** — Large modules like `terraform-aws-modules/vpc` (222 variables) require building Pulumi component equivalents. See [OSS Module Strategies](./migration-approach-comparison.md#5-oss-module-strategies-worst-to-best).
- **Terragrunt** — Terragrunt-managed workspaces declare modules in `.hcl` files, not `.tf` files. Module source mappings must be provided explicitly via `--module-source-map`.

### Group Workspaces That Share Modules

The first workspace that uses a given module requires building the Pulumi component for it. Subsequent workspaces reuse that component via `--module-type-map`. Group workspaces by shared modules so you build each component once and then apply it to all consumers.

### Migration Order Strategy

```
Phase 1: Foundation (1-2 leaf workspaces)
  └── Simple leaf stacks — few modules, common providers, non-production
      └── Validate process, build initial component library
      └── Consume upstream TF state via terraform provider or ESC

Phase 2: Leaf Expansion
  └── Remaining leaf stacks
      └── Reuse components from Phase 1 via --module-type-map
      └── Build new components for newly encountered modules
      └── Validate with --module-schema on second occurrence

Phase 3: Intermediate Stacks
  └── Stacks that are consumed by already-migrated Pulumi stacks
      └── Coordinate reference cutover (see "Migrating Upstream Stacks")
      └── Update downstream stacks from TF state reads → StackReferences
      └── Deploy downstream stacks first, then upstream becomes operable

Phase 4: Foundational Stacks (networking, DNS, IAM)
  └── Core infrastructure with many downstream dependents
      └── Plan the reference cutover across all consuming stacks
      └── Batch downstream updates if there are many consumers
      └── Leverage validated components and established patterns
```

---

## Per-Workspace Migration Steps

### Prerequisites

- **Terraform/OpenTofu initialized in the workspace** (`terraform init` / `tofu init`) — This is required so that the `.terraform/modules/` cache exists. The translation tool reads module sources from this cache rather than downloading them from the network. All remote modules (registry, git, S3, etc.) are resolved by `terraform init`, which authenticates, resolves versions, and downloads them into `.terraform/modules/`. Without this cache, the tool can still translate resources but cannot recover module inputs, outputs, or component schemas.
- A Pulumi project created for the target (`pulumi new <template>`)
- Access to the Terraform state (local file or remote backend)

### Remote and OSS Modules

Remote modules (Terraform Registry, git, S3, GCS, etc.) work automatically — no special handling is needed at translation time. The tool resolves them through the `.terraform/modules/` cache that `terraform init` creates:

```
terraform init   →   .terraform/modules/modules.json   (manifest)
                      .terraform/modules/vpc/           (registry module)
                      .terraform/modules/rds/           (git module)
```

The translation tool reads `modules.json` to find each module's cached directory, then parses the HCL within it to extract variable declarations, output definitions, and type information. This produces the `component-schemas.json` sidecar file with the full typed interface for every module — local and remote alike.

**No network access is needed during translation.** The tool never downloads modules, authenticates to registries, or resolves version constraints. All of that was handled by `terraform init`.

**For building Pulumi components from OSS modules**, the code generation agent should follow the targeted component provider approach (Option 3) from the [Migration Approach Comparison](./migration-approach-comparison.md#5-oss-module-strategies-worst-to-best) — building a Pulumi component scoped to the features actually used by your workspace, rather than wrapping the entire upstream module interface.

### Step 1: Translate State

```bash
pulumi-terraform-migrate stack \
  --from ./terraform-workspace \
  --to ./pulumi-project \
  --out pulumi-state.json \
  --required-providers required-providers.json
```

With module type mappings (for subsequent workspaces):

```bash
pulumi-terraform-migrate stack \
  --from ./terraform-workspace \
  --to ./pulumi-project \
  --out pulumi-state.json \
  --module-type-map module.vpc=myinfra:network:Vpc \
  --module-schema module.vpc=./schemas/vpc-schema.json
```

See the [Pulumi migration guide](https://www.pulumi.com/docs/iac/guides/migration/migrating-to-pulumi/from-terraform/) for full CLI reference.

### Step 2: Install Providers and Import State

```bash
# Install required Pulumi provider plugins
cat required-providers.json | jq -r '.[] | "pulumi plugin install resource \(.name) v\(.version)"' | sh

# Import the translated state
pulumi stack import --file pulumi-state.json
```

See [`pulumi stack import`](https://www.pulumi.com/docs/iac/cli/commands/pulumi_stack_import/) documentation.

### Step 3: Generate Pulumi Code

Generate Pulumi code that matches the imported state. The code generation agent uses:

- **`pulumi-state.json`** — exact inputs, outputs, resource property values, component hierarchy
- **`component-schemas.json`** — typed component interfaces (variable names, Pulumi types, defaults)

The goal: `pulumi preview` shows **no diff** between the generated code and the imported state.

### Step 4: Validate

```bash
# Should show no changes
pulumi preview

# If there are diffs, iterate on the generated code
# Common issues:
#   - Missing input → add to code
#   - Type mismatch → adjust type conversion
#   - Extra resource → check for data sources or generated resources
```

See [`pulumi preview`](https://www.pulumi.com/docs/iac/cli/commands/pulumi_preview/) documentation.

### Step 5: Transfer Ownership

Once `pulumi preview` is clean, the migration for this workspace is complete. Pulumi now owns these resources. See [Operating During Migration](#operating-during-migration) for how to handle the transition period.

---

## Operating During Migration

A migration is rarely all-at-once. You will likely have some workspaces managed by Terraform and others by Pulumi simultaneously.

### The Transition Period

During migration, your infrastructure is split across two control planes:

```
┌─────────────────────────┐    ┌─────────────────────────┐
│       Terraform          │    │        Pulumi            │
│                          │    │                          │
│  networking  (pending)   │    │  app-1  (migrated)       │
│  shared-db   (pending)   │    │  app-2  (migrated)       │
│  app-3       (in flight) │    │                          │
│                          │    │  Reads TF state for      │
│  Still the source of     │    │  vpc_id, subnet_ids      │
│  truth for foundational  │    │  via terraform provider  │
│  infrastructure          │    │  or ESC                  │
└─────────────────────────┘    └─────────────────────────┘
         ▲                              │
         │      data flows this way     │
         └──────────────────────────────┘
         (Pulumi reads FROM Terraform, never writes TO it)
```

### Rules for the Transition Period

**1. Lock migrated Terraform workspaces — don't delete state.**

After importing state into Pulumi and confirming `pulumi preview` shows no diff, **freeze the Terraform workspace** so nobody accidentally runs `terraform apply` against resources Pulumi now owns. Keep the state file intact for rollback:

- **CI/CD:** Remove or disable the Terraform pipeline for migrated workspaces. Add a comment or gate that blocks `terraform plan`/`apply`.
- **Remote backends:** If your backend supports it, restrict write access to the workspace. For example, in Terraform Cloud/Enterprise, lock the workspace via the UI or API.
- **Local convention:** Rename or move the workspace directory (e.g., `terraform-workspace/` → `terraform-workspace.migrated/`) to signal it's no longer active.

Do **not** run `terraform state rm` or delete the state file. The state serves as a rollback target if the Pulumi migration needs to be reversed, and other Terraform workspaces may still read from it via `terraform_remote_state`.

**2. Never run `terraform apply` on migrated resources.**

Running `terraform apply` on resources Pulumi owns creates a split-brain situation where both tools believe they manage the same infrastructure. Changes made by one tool will be invisible to the other.

**3. Data flows from Terraform to Pulumi, never the reverse.**

Migrated Pulumi stacks read from unmigrated Terraform state using the [`terraform` provider](https://www.pulumi.com/registry/packages/terraform/) or [Pulumi ESC](https://www.pulumi.com/docs/esc/). There should be no mechanism for Terraform workspaces to read from Pulumi state — this keeps the dependency graph clean and migration reversible.

### Migrating Upstream Stacks: Coordinated Reference Cutover

Migrating a leaf workspace is low-risk because nothing depends on it. Migrating an **upstream** workspace (e.g., networking) that many downstream stacks read from requires coordination.

**The problem:** After migrating the networking workspace to Pulumi, downstream Pulumi stacks that use the `terraform` provider to read the TF state file will continue to work — the frozen TF state file is still there. But you cannot run `pulumi up` on the upstream networking stack until all downstream stacks have been updated to use Pulumi [stack references](https://www.pulumi.com/docs/iac/concepts/stacks/#stackreferences) instead of Terraform state reads. If you deployed the upstream stack via Pulumi while downstream stacks still point at the old TF state, the TF state would become stale (Pulumi manages the resources now, but the TF state file never gets updated).

**The coordination sequence:**

```
1. Migrate the upstream workspace (networking) to Pulumi
   └── pulumi stack import, generate code, pulumi preview = no diff
   └── Lock the Terraform workspace (freeze TF state)
   └── DO NOT run pulumi up on networking yet

2. Update all downstream Pulumi stacks to use StackReferences
   └── Replace terraform.state.RemoteStateReference with:
       const network = new pulumi.StackReference("org/networking/dev");
       const vpcId = network.getOutput("vpcId");
   └── For each downstream stack: pulumi preview should show no diff
       (the values are identical — it's the same infrastructure)

3. Deploy downstream stacks with updated references
   └── pulumi up on each downstream stack (should be a no-op)
   └── Verify each stack reads from the Pulumi StackReference

4. Now the upstream stack is safe to operate via Pulumi
   └── pulumi up on networking is safe — downstream stacks read
       from Pulumi state, not the frozen TF state
```

**Why the values don't change:** This is a state migration, not an infrastructure change. The VPC ID, subnet IDs, and zone IDs are the same physical resources whether Terraform or Pulumi manages them. The StackReference returns the same values the Terraform state read did. The risk is not incorrect values — it's ensuring no deployment reads from a stale source.

**For stacks with many dependents:** Consider batching the downstream updates. You don't need to update all downstream stacks simultaneously — the frozen TF state continues to serve correct (if static) values. The constraint is only that all downstream stacks must be updated before you make any *changes* to the upstream stack via Pulumi.

### Pipelines for Mixed Environments

During migration, your CI/CD pipelines need to handle both Terraform and Pulumi stacks. A typical pattern:

```
┌─────────────────────────────────────────────────────────┐
│                    CI/CD Pipeline                         │
│                                                          │
│  For each workspace/stack:                               │
│    if terraform workspace (not yet migrated):             │
│      terraform plan → terraform apply                    │
│    if terraform workspace (migrated, frozen):             │
│      skip (or: terraform plan with no-apply gate)        │
│    if pulumi stack:                                       │
│      pulumi preview → pulumi up                          │
│                                                          │
│  Deploy order respects dependency graph:                  │
│    networking (TF) → shared-db (TF) → app-1 (Pulumi)    │
│                                                          │
│  After upstream migration + ref cutover:                  │
│    networking (Pulumi) → shared-db (Pulumi) → app-1      │
└─────────────────────────────────────────────────────────┘
```

The key principle: deploy order follows the dependency graph regardless of which tool manages each stack. Upstream stacks deploy first so their outputs are available to downstream stacks.

### Building Reusable Components as Component Providers

When the translation tool encounters a Terraform module, it produces a `component-schemas.json` file describing the module's typed interface. Use this to build **component providers** — standalone Pulumi provider plugins (`pulumi-resource-<name>`) that expose your components as multi-language packages with full schemas.

Building components as providers (rather than inline `ComponentResource` classes) has concrete benefits for the migration:

- **Schema generation feeds back into the migration.** Once you build the provider binary, extract its schema with `pulumi package get-schema` and pass it back to the translation tool via `--module-schema`. This validates that subsequent workspaces' module usage is compatible with your component's interface — catching mismatches at translation time, before code generation.
- **Multi-language support.** Component providers are consumable from any Pulumi language (TypeScript, Python, Go, C#, Java, YAML). If different teams use different languages, the component works everywhere.
- **Publish to the Pulumi Cloud component registry.** Component providers can be published to your organization's [private registry](https://www.pulumi.com/docs/iac/concepts/packages/), giving you centralized documentation, visibility into which stacks use which version, and integration into the Pulumi Cloud data model (resource search, compliance views, audit trails).
- **Formal interface.** The schema defines inputs, outputs, types, and defaults — the same information the code generation agent needs to produce correct code.

See [Migration Approach Comparison: Component Providers and Schema-Driven Reuse](./migration-approach-comparison.md#6-component-providers-and-schema-driven-reuse) for details on building and distributing component providers.

### Shared Modules Across Workspaces

When multiple Terraform workspaces use the same module (e.g., your internal VPC module), you build the Pulumi component provider once and reuse it:

```bash
# First workspace: auto-derives component type, agent builds component provider
pulumi-terraform-migrate stack --from ./workspace-1 --to ./pulumi-1 --out state.json
# → agent uses component-schemas.json to build pulumi-resource-myinfra
# → extract schema: pulumi package get-schema ./bin/pulumi-resource-myinfra > schema.json

# Second workspace: map to existing component type + validate against schema
pulumi-terraform-migrate stack --from ./workspace-2 --to ./pulumi-2 --out state.json \
  --module-type-map module.vpc=myinfra:network:Vpc \
  --module-schema module.vpc=./schema.json
```

If a later workspace uses module features not covered by the existing component provider, `--module-schema` validation catches it at translation time — before code generation begins. Expand the component provider's interface, rebuild, and re-extract the schema.

---

## Completing the Migration

### End State

A fully migrated organization has:

- **All infrastructure managed by Pulumi** — `pulumi preview` shows no diff on every stack
- **Terraform state archived or deleted** — no resources tracked by both tools
- **Cross-stack references using StackReferences** — no remaining reads from Terraform state files
- **Reusable component provider library** — Pulumi component providers built from TF modules, with schemas, usable across stacks and languages
- **Secrets properly encrypted** — sensitive values that were plaintext in TF state are now encrypted by Pulumi's secrets provider

### Post-Migration Cleanup

1. **Archive Terraform state backends** — Move S3 buckets, Azure containers, or GCS buckets to archive storage. Keep them for a retention period per your organization's policy.

2. **Remove Terraform configuration** — Archive `.tf` files and `.terraform/` directories. These are no longer the source of truth.

3. **Update CI/CD pipelines** — Replace `terraform plan`/`apply` with `pulumi preview`/`up`. See [Pulumi CI/CD integrations](https://www.pulumi.com/docs/iac/guides/continuous-delivery/).

4. **Configure Pulumi access controls** — Set up [teams, RBAC, and audit logging](https://www.pulumi.com/docs/administration/access-identity/) in Pulumi Cloud.

5. **Set up drift detection** — Enable [Pulumi Deployments drift detection](https://www.pulumi.com/docs/deployments/deployments/drift/) to catch out-of-band changes (the equivalent of `terraform plan` in CI).

### Rollback

If a migration needs to be reversed:

- Pulumi state can be exported ([`pulumi stack export`](https://www.pulumi.com/docs/iac/cli/commands/pulumi_stack_export/)) and the resources can be imported back into Terraform using `terraform import`
- The original Terraform state (if archived) can be restored
- Physical infrastructure is never modified during state translation — rollback is purely a control-plane operation

---

## Further Reading

- [Pulumi: Migrating from Terraform](https://www.pulumi.com/docs/iac/guides/migration/migrating-to-pulumi/from-terraform/) — official migration documentation
- [Pulumi State and Backends](https://www.pulumi.com/docs/iac/concepts/state-and-backends/) — how Pulumi stores and manages state
- [Pulumi Secrets](https://www.pulumi.com/docs/iac/concepts/secrets/) — configuring encryption providers
- [Pulumi Component Resources](https://www.pulumi.com/docs/iac/concepts/components/) — building reusable infrastructure abstractions
- [Pulumi Stack References](https://www.pulumi.com/docs/iac/concepts/stacks/#stackreferences) — cross-stack dependencies (replaces `terraform_remote_state`)
- [Pulumi ESC](https://www.pulumi.com/docs/esc/) — environments, secrets, and configuration for cross-stack data sharing
- [Pulumi Terraform Provider](https://www.pulumi.com/registry/packages/terraform/) — read Terraform state from Pulumi programs
- [Terraform: Sensitive Data in State](https://developer.hashicorp.com/terraform/language/state/sensitive-data) — HashiCorp's documentation on state security
- [Migration Approach Comparison](./migration-approach-comparison.md) — state-first vs. code-first, OSS module strategies
