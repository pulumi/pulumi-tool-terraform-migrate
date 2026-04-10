# Refactor to Components

Restructure flat Pulumi state (imported from Terraform) into component resources using `module-map.json` as the blueprint.

## Prerequisites

- `module-map.json` exists (produced by `pulumi-terraform-migrate module-map`)
- Target language chosen: TypeScript or Python
- Required Pulumi plugins installed (see Step 0)

## Workflow

### Step 0: Install required plugins

When using Path A, generate `required-plugins.json` alongside the translated state:

```bash
pulumi-terraform-migrate stack \
  --from <tf-dir> --to <pulumi-dir> --out state.json \
  --plugins required-plugins.json
```

For both paths, install the plugins before running any Pulumi commands:

```bash
# Read required-plugins.json and install each plugin
cat required-plugins.json | jq -r '.[] | "pulumi plugin install resource \(.name) \(.version)"' | sh
```

This ensures the correct provider versions are available. Without this step, `pulumi preview` and `pulumi import` may download mismatched provider versions.

### Step 1: Load module-map.json

Parse the file and present an inventory table to the user:

| Module | Resources | Inputs | Outputs | Index Type |
|--------|-----------|--------|---------|------------|
| module.vpc | 12 | 5 | 3 | none |
| module.subnet[0] | 4 | 3 | 1 | count |

Use the schema documented in [references/module-map-format.md](references/module-map-format.md).

### Step 2: Choose migration path

Present both paths and let the user choose:

**Path A: Translate + Alias (flat-state-first)**
- Translate TF state to flat Pulumi state (`pulumi-terraform-migrate stack`)
- Import flat state (`pulumi stack import`)
- Build components, wire aliases, `pulumi up` to restructure
- **Tradeoffs:** Simpler — works with existing state and existing tooling. Requires alias wiring and a cleanup step to remove aliases after migration.
- **Prerequisites:** Pulumi state already imported into stack via `pulumi-terraform-migrate stack`

**Path B: Direct Import into Components (code-first)**
- Build Pulumi program with component structure first
- Use `pulumi import` with import IDs from `module-map.json` to import directly into the component structure
- **Tradeoffs:** Cleaner result — no aliases, no intermediate flat state, no cleanup step. Requires the program to be correct before importing (preview must succeed before import).
- **Prerequisites:** None beyond the module-map

### Step 3: Component mapping review

Default mapping: 1:1 Terraform module to Pulumi component.

Offer the user these adjustments:
- **Merge modules** — combine multiple TF modules into one component
- **Keep flat** — skip componentization for a module, leave resources at root
- **Map to existing component** — use a published component (e.g., `@pulumi/awsx:ec2:Vpc`); see [references/existing-component-integration.md](references/existing-component-integration.md)
- **Move resources** — reassign specific URNs between groups

Maintain the working plan in conversation context. Do not write intermediate files.

### Step 4: Per-module generation loop

For each module in the approved plan:

1. **Propose** to the user:
   - Component class name and type token (e.g., `my:components:Vpc`)
   - Args interface derived from `interface.inputs`
   - Child resources (list from `resources`)

2. **User approves or adjusts.**

3. **Generate component class:**
   - Subclass `ComponentResource` (TS) or `pulumi.ComponentResource` (Python)
   - Constructor accepts args, creates child resources, calls `registerOutputs`
   - Component class is clean — NO migration aliases or transforms inside it

4. **Checkpoint** — confirm with user before moving to next module.

For 15+ structurally similar modules (same source, same interface), offer **batch mode**: generate one template, apply to all instances.

### Step 5: Generate main program

- Read `evaluatedValue` from module-map inputs for concrete values
- Read `expression` to understand derivation — prefer variable references over hardcoded values where the expression references a `var.*` or another module output
- Instantiate each component with appropriate args

**Path A only:** Wire migration aliases using the transform pattern from [references/alias-wiring-pattern.md](references/alias-wiring-pattern.md). Generate `migration-aliases.json` mapping new child resource names to old flat URNs (from `resources[].translatedUrn`).

**Path B only:** No alias wiring needed. Components are instantiated cleanly.

### Step 6: Import / Verification

**Path A: Translate + Alias**

1. `pulumi preview` — expect zero changes (aliases resolve old URNs to new component children)
2. `pulumi up` — state updated with component hierarchy
3. Delete `migration-aliases.json` and remove transform code from main program
4. `pulumi preview` — still zero changes (state now reflects new URNs)

**Path B: Direct Import**

1. `pulumi preview --import-file import.json` — generates import skeleton mapping each resource to its type and name
2. Fill in `id` values in `import.json` from module-map's `resources[].importId` (match by type token and name, or by `resources[].terraformAddress`)
3. `pulumi import --file import.json` — imports cloud resources directly into the component structure
4. `pulumi preview` — expect zero changes (state matches program)

## Notes

- Reference the `pulumi-component` skill for component authoring patterns if available in the workspace.
- Do not embed code templates in this skill. The agent generates code from module-map data at runtime.
- Component classes must be migration-unaware. All alias wiring is external via transforms on the component instantiation.
- **Child resource naming**: Child resources inside components must use unique logical names that incorporate the parent name. For example, `new aws.s3.Bucket(`${name}-bucket`, ...)` inside `BucketComponent("bucket-0")` produces child name `bucket-0-bucket`. Using a bare name like `"bucket"` causes URN collisions when multiple component instances exist.
- **Alias format (Path A)**: Migration aliases must be plain URN strings, not objects. Use `aliases: [...existing, oldUrn]` — not `{ urn: oldUrn }`. The Pulumi `Alias` interface does not have a `urn` field.
- **Plugin installation**: Always generate and install `required-plugins.json` before running Pulumi commands. The `--plugins` flag on the `stack` command produces this file. Without it, Pulumi may download different provider versions than the Terraform state was built with.
