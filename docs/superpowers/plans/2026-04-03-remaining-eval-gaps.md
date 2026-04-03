# Remaining Eval Gaps — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the remaining ~152 eval warnings when translating real-world TF projects, focusing on the two fixable root causes.

**Architecture:** Two branches targeting the two highest-impact gaps. A third category (conditional/counted resources not in state) is expected behavior and not fixable.

**Tech Stack:** Go, `hashicorp/hcl/v2`, `zclconf/go-cty`, existing `pkg/hcl` and `pkg/component_populate.go`.

---

## Warning Analysis (DNS-to-DB, 152 warnings)

| Root Cause | Count | Example | Fixable? |
|-----------|-------|---------|----------|
| **Unsupported attribute on module outputs/resources** | 90 | `module.vpc.private_subnets` attr not found; `aws_acm_certificate.this` attr not found inside module | Partially — module cross-ref values need proper attribute access; counted resources (`this[0]`) need index support |
| **Unknown variable `module`** | 28 | `module.loadbalancer_sg.security_group_id` not in cross-ref map because the output itself failed to evaluate | Yes — cascading failure from unresolved module outputs. Fixing output eval fixes these |
| **Unknown variable `local`** | 22 | `local.common_tags` in root call sites — root locals fail because `var.business_divsion` not in eval context | Yes — parse root variable defaults and merge into tfvars |
| **Unknown variable (resources)** | 12 | `aws_ebs_volume`, `aws_customer_gateway` — resources not in state (gated by `count = 0`) | No — expected behavior, resources were never created |

---

## Branch Strategy

| Branch | Base | Scope | Est. lines |
|--------|------|-------|-----------|
| `feat/mc-09h-root-var-defaults` | `feat/mc-09g-nested-callsites` | Parse root variable defaults, merge into tfvars for locals evaluation | ~80 |
| `feat/mc-09i-indexed-resource-refs` | `feat/mc-09h-root-var-defaults` | Support `resource[0]` and `module.X.output` attribute access in scoped resource/output maps | ~150 |

Restack `feat/mc-10-discovery-acceptance` onto `feat/mc-09i` after.

---

## PR 09h: Root Variable Defaults

### Problem

Root-level `variable` blocks have defaults (e.g., `variable "environment" { default = "dev" }`). These defaults aren't loaded into the eval context because we only load `terraform.tfvars` + `*.auto.tfvars`. When root locals reference these variables (e.g., `local.name = "${var.business_divsion}-${var.environment}"`), the locals evaluation fails, and all downstream `local.*` refs fail (22 warnings for `tags`, `name`, etc.).

### Fix

Parse root variable declarations from `tfSourceDir`, extract defaults, and merge them into the tfvars map (tfvars values take precedence over defaults).

### File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `pkg/hcl/parser.go` | Already has `ParseModuleVariables` — reuse for root dir |
| Modify | `pkg/component_populate.go` | Parse root variables, merge defaults into tfvars before locals evaluation |
| Modify | `pkg/component_populate_test.go` | Test that root variable defaults enable locals resolution |

### Task 1: Merge root variable defaults into tfvars

- [ ] **Step 1: Write failing test**

```go
func TestPopulateComponentsFromHCL_RootVariableDefaults(t *testing.T) {
	// root_with_locals has: variable "env" { default = "dev" }
	// locals { name = "${var.project}-${var.env}" }
	// module "pet" { prefix = local.name }
	// Without root var defaults, local.name fails → prefix uses default from pet module
	// With root var defaults, local.name = "myapp-dev" → prefix = "myapp-dev"
	components := []PulumiResource{...}
	// Verify prefix input is "myapp-dev" (from local.name using var defaults)
}
```

- [ ] **Step 2: Implement**

In `populateComponentsFromHCL`, after loading tfvars and before evaluating locals:

```go
// Merge root variable defaults into tfvars (tfvars values take precedence)
rootVars, _ := hclpkg.ParseModuleVariables(tfSourceDir)
for _, v := range rootVars {
	if _, alreadySet := tfvars[v.Name]; !alreadySet && v.Default != nil {
		tfvars[v.Name] = *v.Default
	}
}
```

- [ ] **Step 3: Run tests, commit**

```bash
git commit -m "feat: merge root variable defaults into tfvars for locals evaluation"
```

---

## PR 09i: Indexed Resource and Module Output References

### Problem

Two related issues:

1. **Counted/indexed resources inside modules**: Output expressions like `aws_acm_certificate.this[0].arn` reference a counted resource. The scoped attr map has the resource keyed by the address `module.acm.aws_acm_certificate.this[0]`, but the lookup strips only `module.acm` and looks for `aws_acm_certificate.this[0]` as a type.name pair — failing because `this[0]` isn't a valid resource name key.

2. **Module output cross-refs with attribute access**: When `module.vpc.private_subnets` is resolved to a list value via module cross-refs, expressions like `element(module.vpc.private_subnets, 0)` or `module.vpc.private_subnets[0]` fail because the cross-ref value is a flat cty.List, and HCL index expressions need to work on it.

### Fix for counted resources

`parseResourceAddress` needs to handle indexed resource names. For `module.acm.aws_acm_certificate.this[0]`, it should parse:
- modulePath: `module.acm`
- resType: `aws_acm_certificate`
- resName: `this[0]`

And the scoped attr map should store both `this[0]` and `this` entries so both `resource.this.attr` and `resource.this[0].attr` patterns resolve.

### Fix for module output cross-refs

The module cross-ref pre-pass evaluates output expressions to concrete `cty.Value`s (e.g., a `cty.ListVal` for `private_subnets`). These are stored in `moduleOutputValues` and added to the eval context via `NewEvalContext(..., moduleOutputValues)`. The HCL evaluator should handle index expressions on list values natively — but the issue might be that the output value is a tuple (from `cty.TupleVal`) which doesn't support index access the same way.

Need to investigate: is the issue in how we store the value, or in how HCL evaluates the index expression?

### File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `pkg/component_populate.go` | Fix `parseResourceAddress` for indexed names; investigate module output indexing |
| Modify | `pkg/component_populate_test.go` | Tests for indexed resource attr lookup |

### Task 2: Handle indexed resource names in scoped attrs

- [ ] **Step 1: Write failing test**

```go
func TestParseResourceAddress_Indexed(t *testing.T) {
	mod, typ, name := parseResourceAddress("module.acm.aws_acm_certificate.this[0]")
	require.Equal(t, "module.acm", mod)
	require.Equal(t, "aws_acm_certificate", typ)
	require.Equal(t, "this[0]", name)
}
```

- [ ] **Step 2: Fix `parseResourceAddress`**

The current implementation splits on `.` which breaks for `this[0]` (no dots). Actually, `this[0]` has no dots, so the current split should work — `parts[-1]` would be `this[0]`. But the TF state JSON represents counted instances differently — need to check how `tfjson.StateResource.Address` formats them.

- [ ] **Step 3: Store both indexed and base keys**

When inserting into the scoped attr map, if the name contains `[`, also store under the base name:
```go
result[modulePath][resType][resName] = cty.ObjectVal(attrs) // "this[0]"
baseName := resName
if idx := strings.Index(resName, "["); idx > 0 {
	baseName = resName[:idx]
}
if baseName != resName {
	result[modulePath][resType][baseName] = cty.ObjectVal(attrs) // "this" (overwrites, last wins)
}
```

- [ ] **Step 4: Run tests, commit**

```bash
git commit -m "feat: handle indexed resource names in scoped attribute map"
```

### Task 3: Investigate and fix module output index access

- [ ] **Step 1: Debug**

Add temporary logging to see what type the module output value has and why index access fails.

- [ ] **Step 2: Fix if needed**

If the output value is stored as a `cty.TupleVal` instead of `cty.ListVal`, HCL's index expressions might not work. Convert tuple outputs to lists where possible.

- [ ] **Step 3: Run tests, commit**

---

## Expected Outcome

After both PRs, the DNS-to-DB warning count should drop significantly:
- **22 `local.*` warnings → ~0** (root var defaults fix)
- **28 `module.*` unknown → reduced** (cascading fix from better output eval)
- **90 unsupported attr → reduced** (indexed resource names + better scoping)
- **12 missing resources → unchanged** (expected, conditional resources not in state)

Target: ~30-50 remaining warnings (down from 152), primarily from conditional resources and edge cases in complex output expressions.

---

## Diminishing Returns Note

The current state (1181 inputs, 383 outputs, full typed metadata) provides excellent coverage for the code generation agent. These fixes improve output quality but have decreasing marginal value. After these two PRs, remaining warnings will be from genuinely unresolvable cases (resources not in state, complex expression patterns). Further work should focus on the agent consuming the output rather than perfecting the translator.
