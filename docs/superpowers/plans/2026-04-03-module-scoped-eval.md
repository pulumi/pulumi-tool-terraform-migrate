# Module-Scoped Eval Context — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix remaining eval context scoping so output expressions inside remote modules resolve, internal locals are parsed, and nested module call sites are evaluated.

**Architecture:** Three independent fixes to `populateComponentsFromHCL` in `pkg/component_populate.go`. Each addresses a different scoping problem: (1) resource attributes keyed by full address with module-prefix filtering, (2) locals parsed per-module not just root, (3) call sites parsed recursively for nested modules.

**Tech Stack:** Go, `hashicorp/hcl/v2`, `zclconf/go-cty`, existing `pkg/hcl` and `pkg/tofu` packages.

**TDD order for every task:** Write failing test → run to verify failure → implement → verify pass → commit.

---

## Branch Strategy

| Branch | Base | Scope | Est. lines |
|--------|------|-------|-----------|
| `feat/mc-09e-module-scoped-attrs` | `feat/mc-09d-eval-context` | Fix `buildResourceAttrMap` to key by full address, fix `buildModuleScopedResourceAttrs` to filter by module prefix | ~150 |
| `feat/mc-09f-remote-locals` | `feat/mc-09e-module-scoped-attrs` | Parse locals from each resolved module source dir, not just root | ~100 |
| `feat/mc-09g-nested-callsites` | `feat/mc-09f-remote-locals` | Parse call sites from parent module dirs for nested modules | ~200 |

Restack `feat/mc-10-discovery-acceptance` onto `feat/mc-09g-nested-callsites` after.

---

## PR 09e: Module-Scoped Resource Attributes

### Problem

`buildResourceAttrMap` strips module prefixes and keys by short type/name (e.g., `aws_vpc` → `this`). When multiple modules have resources of the same type and name (common — many modules use `this` as the resource name), they collide. More critically, output expressions inside a module (e.g., `output "vpc_id" { value = aws_vpc.this.id }` inside the vpc module) can't resolve because the global attr map has `aws_vpc.this` from ALL modules mixed together.

### Fix

1. **Change `buildResourceAttrMap`** to key by full address prefix: `modulePath → type → name → attrs`. The top-level map key is the module path (e.g., `"module.vpc"`, `""` for root).
2. **Fix `buildModuleScopedResourceAttrs`** to return only the attrs for a specific module path.

### File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `pkg/component_populate.go` | Rebuild `buildResourceAttrMap` with module-prefix keys, fix `buildModuleScopedResourceAttrs` |
| Modify | `pkg/component_populate_test.go` | Test scoped attr lookup |

### Task 1: Rebuild resource attr map with module scoping

**Files:** `pkg/component_populate.go`, `pkg/component_populate_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestBuildModuleScopedResourceAttrs(t *testing.T) {
	// Two modules both have "random_pet.this" — scoped lookup should return only the correct one
	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/tofu_state_indexed_modules.json",
	})
	require.NoError(t, err)

	fullAttrs := buildResourceAttrMap(tfState)

	// module.pet[0] has random_pet.this with prefix "test-0"
	pet0Attrs := buildModuleScopedResourceAttrs(fullAttrs, "module.pet[0]")
	require.NotNil(t, pet0Attrs)
	petType, ok := pet0Attrs["random_pet"]
	require.True(t, ok, "should have random_pet in module.pet[0] scope")
	petThis, ok := petType["this"]
	require.True(t, ok)
	// The pet in module.pet[0] should have prefix "test-0"
	prefix := petThis.GetAttr("prefix")
	require.True(t, prefix.IsKnown())
	require.Equal(t, "test-0", prefix.AsString())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/ -run "TestBuildModuleScopedResourceAttrs" -v`
Expected: FAIL (current impl returns global map, both pet[0] and pet[1] attrs mixed)

- [ ] **Step 3: Implement**

Change `buildResourceAttrMap` return type to `map[string]map[string]map[string]cty.Value` (modulePath → type → name → attrs):

```go
// buildResourceAttrMap builds a module-scoped resource attribute map.
// Returns modulePath → resourceType → resourceName → cty.ObjectVal(attributes).
// Root module resources have modulePath "".
func buildResourceAttrMap(tfState *tfjson.State) map[string]map[string]map[string]cty.Value {
	result := map[string]map[string]map[string]cty.Value{}
	if tfState == nil {
		return result
	}

	tofu.VisitResources(tfState, func(r *tfjson.StateResource) error {
		if r.AttributeValues == nil {
			return nil
		}
		// Extract module path and resource type.name from address
		// "module.vpc.aws_vpc.this" → modulePath="module.vpc", type="aws_vpc", name="this"
		// "aws_s3_bucket.mybucket" → modulePath="", type="aws_s3_bucket", name="mybucket"
		modulePath, resType, resName := parseResourceAddress(r.Address)

		attrs := map[string]cty.Value{}
		for k, v := range r.AttributeValues {
			attrs[k] = interfaceToCty(v)
		}
		if _, ok := result[modulePath]; !ok {
			result[modulePath] = map[string]map[string]cty.Value{}
		}
		if _, ok := result[modulePath][resType]; !ok {
			result[modulePath][resType] = map[string]cty.Value{}
		}
		result[modulePath][resType][resName] = cty.ObjectVal(attrs)
		return nil
	}, &tofu.VisitOptions{})

	return result
}

// parseResourceAddress splits "module.vpc.aws_vpc.this" into ("module.vpc", "aws_vpc", "this").
// Root resources like "aws_s3_bucket.this" return ("", "aws_s3_bucket", "this").
func parseResourceAddress(address string) (modulePath, resType, resName string) {
	parts := strings.Split(address, ".")
	// Find where the module path ends and resource type.name begins
	// Walk from the end: last two parts are always type.name
	if len(parts) < 2 {
		return "", address, ""
	}
	resName = parts[len(parts)-1]
	resType = parts[len(parts)-2]

	// Everything before type.name is the module path
	if len(parts) > 2 {
		modulePath = strings.Join(parts[:len(parts)-2], ".")
	}
	return modulePath, resType, resName
}
```

Fix `buildModuleScopedResourceAttrs`:

```go
func buildModuleScopedResourceAttrs(
	allAttrs map[string]map[string]map[string]cty.Value,
	modulePath string,
) map[string]map[string]cty.Value {
	if allAttrs == nil {
		return nil
	}
	if scoped, ok := allAttrs[modulePath]; ok {
		return scoped
	}
	return nil
}
```

Update all callers to use the new 3-level map. The `NewEvalContext` `resources` parameter stays `map[string]map[string]cty.Value` (type → name → attrs) — we just select the right module slice before passing it.

- [ ] **Step 4: Update callers**

In `populateComponentsFromHCL`:
- Root-level input evaluation: pass `allAttrs[""]` (root module resources) to `NewEvalContext`
- Output evaluation: pass `buildModuleScopedResourceAttrs(allAttrs, node.modulePath)`

Also update `buildDataSourceAttrMap` with the same module-scoping approach.

- [ ] **Step 5: Run tests, commit**

Run: `go test ./pkg/... -count=1`

```bash
git add pkg/component_populate.go pkg/component_populate_test.go
git commit -m "feat: scope resource attributes by module path in eval context"
```

---

## PR 09f: Remote Module Internal Locals

### Problem

Currently `ParseLocals` and `evaluateLocals` only run against the root `tfSourceDir`. Remote modules (e.g., vpc, security-group) have their own `locals` blocks that define values like `local.this_sg_id`, `local.create`. Output expressions inside these modules reference these internal locals.

### Fix

In the output evaluation section of `populateComponentsFromHCL`, parse and evaluate locals from the module's `sourcePath` directory (not just root). Add them to the output eval context.

### File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `pkg/component_populate.go` | Parse + evaluate locals per module source dir for output eval |
| Modify | `pkg/component_populate_test.go` | Test with fixture that has module-internal locals |

### Task 2: Parse locals from module source dirs

**Files:** `pkg/component_populate.go`, `pkg/component_populate_test.go`

- [ ] **Step 1: Create test fixture**

Create `pkg/hcl/testdata/module_with_locals/main.tf`:
```hcl
variable "prefix" { type = string }
locals {
  full_name = "${var.prefix}-local"
}
resource "random_pet" "this" { prefix = local.full_name }
output "name" { value = random_pet.this.id }
output "full_name" { value = local.full_name }
```

Create `pkg/hcl/testdata/root_calling_module_with_locals/main.tf`:
```hcl
module "pet" {
  source = "../module_with_locals"
  prefix = "test"
}
```

- [ ] **Step 2: Write failing test**

```go
func TestPopulateComponentsFromHCL_ModuleInternalLocals(t *testing.T) {
	// The module has locals { full_name = "${var.prefix}-local" }
	// and output "full_name" { value = local.full_name }
	// With prefix="test", full_name should be "test-local"
	components := []PulumiResource{
		{PulumiResourceID: PulumiResourceID{Name: "pet", Type: "terraform:module/pet:Pet"}},
	}
	tree := []*componentNode{
		{name: "pet", resourceName: "pet", typeToken: "terraform:module/pet:Pet", modulePath: "module.pet"},
	}

	resourceAttrs := // ... build from test state with random_pet.this attrs
	metadata, err := populateComponentsFromHCL(components, tree, nil, nil,
		"hcl/testdata/root_calling_module_with_locals", true, resourceAttrs, nil)
	require.NoError(t, err)

	outputs := components[0].Outputs
	require.Contains(t, outputs, resource.PropertyKey("full_name"))
	require.Equal(t, resource.NewStringProperty("test-local"), outputs["full_name"])
}
```

- [ ] **Step 3: Implement**

In the output evaluation section (around line 207-242), after building `outputEvalCtx`, parse and evaluate module-internal locals:

```go
// Parse and evaluate module-internal locals
moduleDefs, _ := hclpkg.ParseLocals(sourcePath)
if len(moduleDefs) > 0 {
	moduleLocalValues := evaluateLocals(moduleDefs, outputEvalCtx)
	if len(moduleLocalValues) > 0 {
		outputEvalCtx.AddVariables(map[string]cty.Value{"local": cty.ObjectVal(moduleLocalValues)})
	}
}
```

Same for the pre-pass output evaluation (around line 98-115).

- [ ] **Step 4: Run tests, commit**

```bash
git add pkg/component_populate.go pkg/component_populate_test.go pkg/hcl/testdata/
git commit -m "feat: parse and evaluate locals from remote module source dirs"
```

---

## PR 09g: Nested Module Call Site Parsing

### Problem

`populateComponentsFromHCL` only parses call sites from the root `tfSourceDir`. For nested modules like `module.rdsdb.module.db_subnet_group`, the call site is inside the `rdsdb` module source (not the root). So nested modules get 0 inputs.

### Fix

For nested components (those with a parent component), resolve the parent's source path, parse call sites from that directory, and evaluate the nested module's arguments.

### File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `pkg/component_populate.go` | For nested components, parse call sites from parent module source dir |
| Modify | `pkg/component_populate_test.go` | Test nested module input population |

### Task 3: Parse call sites from parent module dirs

**Files:** `pkg/component_populate.go`, `pkg/component_populate_test.go`

- [ ] **Step 1: Write failing test**

Use the deep_nested_mixed fixture or create a targeted one:
```go
func TestPopulateComponentsFromHCL_NestedCallSites(t *testing.T) {
	// module.env["dev"].module.svc[0] — the svc call site is inside the env module
	// Verify svc component gets inputs from the parent module's call site
}
```

- [ ] **Step 2: Implement**

Currently, `callSiteMap` is built from `ParseModuleCallSites(tfSourceDir)` — root only. For nested modules:

1. Build a `parentSourceMap` from `resolvedSources` — maps module name to source path.
2. For each component, if it's a child (has a parent in the component tree), look up the parent's source path and parse call sites from there.
3. Cache parsed call sites per source dir to avoid re-parsing.

```go
// Cache of parsed call sites per directory
callSiteCaches := map[string]map[string]*hclpkg.ModuleCallSite{}
callSiteCaches[tfSourceDir] = callSiteMap // root already parsed

// For each component:
if !hasCallSite {
	// Check if parent module has call sites for this nested module
	parentNode := findParentNode(componentTree, node)
	if parentNode != nil {
		parentSource := resolvedSources["module."+parentNode.name]
		if parentSource != "" {
			if _, cached := callSiteCaches[parentSource]; !cached {
				parentCalls, _ := hclpkg.ParseModuleCallSites(parentSource)
				cache := map[string]*hclpkg.ModuleCallSite{}
				for i := range parentCalls {
					cache[parentCalls[i].Name] = &parentCalls[i]
				}
				callSiteCaches[parentSource] = cache
			}
			callSite = callSiteCaches[parentSource][node.name]
			hasCallSite = callSite != nil
		}
	}
}
```

Also need a `findParentNode` helper that finds the parent `componentNode` for a nested component.

- [ ] **Step 3: Run tests, commit**

```bash
git add pkg/component_populate.go pkg/component_populate_test.go
git commit -m "feat: parse call sites from parent module dirs for nested modules"
```

---

## Verification

After all three PRs, run against DNS-to-DB with module cache:

```bash
/tmp/pulumi-terraform-migrate stack \
  --from pkg/testdata/tf_dns_to_db \
  --state-file pkg/testdata/tofu_state_dns_to_db.json \
  --to /tmp/out --out /tmp/out/state.json --plugins /tmp/out/plugins.json \
  --pulumi-stack dev --pulumi-project test 2>/tmp/out/stderr.txt

# Count remaining warnings (should be significantly fewer than 130)
grep -c "Warning:" /tmp/out/stderr.txt
```

**Expected improvements:**
- PR 09e: VPC module outputs like `vpc_id`, `public_subnets` should resolve (was: ~96 output failures from unscoped attrs)
- PR 09f: Security group outputs referencing `local.this_sg_id` should resolve (was: failures from missing internal locals)
- PR 09g: Nested rdsdb submodules (db_option_group, db_parameter_group, db_subnet_group) should get inputs (was: 0 inputs)
