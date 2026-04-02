# Terraform Modules to Pulumi Component Resources — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Translate Terraform module hierarchy into Pulumi ComponentResource entries in migrated state.

**Architecture:** Phase 1 modifies the state translation pipeline to create `Custom: false` component resources for each TF module, establishing parent-child relationships. Phase 2 adds HCL parsing using the existing `github.com/pulumi/opentofu` fork (which exposes Terraform's internal packages including the function library), populating component inputs/outputs from module variable/output declarations and call-site expressions.

**Tech Stack:** Go, `hashicorp/hcl/v2`, `hashicorp/terraform-json`, `zclconf/go-cty`, `github.com/pulumi/opentofu` (for HCL functions), Pulumi SDK.

**Default behavior:** `enableComponents` defaults to `true`. Existing tests that use module test fixtures will be updated to expect component resources. The `--no-module-components` flag opts out.

**Spec:** `docs/superpowers/specs/2026-03-31-module-to-component-design.md` (on branch `feat/module-to-component-design`)

**TDD order for every task:** Create test fixtures → write failing tests → implement code → verify tests pass → commit.

---

## PR Strategy

10 stacked PRs, each small and independently reviewable.

### Phase 1: Component Resource Structure (PRs 1-5)

| PR | Branch | Base | Scope | Status |
|----|--------|------|-------|--------|
| 1 | `feat/mc-01-migration-schema` | `main` | Migration file schema + type token derivation + module address parsing | **DONE** |
| 2 | `feat/mc-02-component-tree` | PR 1 | Component tree builder + collision detection | **DONE** |
| 3 | `feat/mc-03-pulumi-state` | PR 2 | `PulumiState` struct changes + component insertion into deployment | **DONE** |
| 4 | `feat/mc-04-pipeline-integration` | PR 3 | Integration into `convertState` pipeline + CLI flags + existing test updates | **DONE** |
| 4.5 | `feat/mc-04b-schema-validation` | PR 4 | Schema validation support — load Pulumi package schema, validate component interface | **DONE** |
| 5 | `feat/mc-05-integration-tests` | PR 4.5 | Test fixtures from real deployments for indexed/keyed modules | **DONE** |

**Implementation notes (PRs 1-5):**
- `buildComponentTree` returns `([]*componentNode, error)` (collision detection integrated from the start).
- `PulumiResource.Parent` field was added in PR 2 (needed by `toComponents`), not PR 3 as originally planned.
- `PulumiNameFromTerraformAddress` now takes a `useShortName bool` third parameter. All existing call sites pass `false`.
- `TranslateAndWriteState`, `TranslateState`, and `convertState` all accept `enableComponents bool` and `typeOverrides map[string]string` parameters.
- Unit tests for `convertState` pass `enableComponents: false` to preserve existing behavior. The `translateStateFromJson` test helper passes `enableComponents: true`.
- Schema validation uses `github.com/pulumi/pulumi/pkg/v3/codegen/schema` (already in `go.mod`). `LoadComponentSchema` + `ValidateAgainstSchema` validate field presence (not types — types handled by value conversion pipeline).
- Real deployment fixtures for indexed modules (`tofu_state_indexed_modules.json`) and keyed modules (`tofu_state_keyed_modules.json`) captured from `tofu apply`.

### Phase 2: HCL Parsing & Input/Output Population (PRs 6-10)

**OSS module strategy note:** OSS/third-party TF modules are handled via agent-driven strategy (terraform-module plugin or custom component). Both produce a Pulumi package schema JSON. The tool's contract is: optionally accept a schema for validation; always derive values from HCL/state. See PR 4.5 for schema validation support.

| PR | Branch | Base | Scope | Status |
|----|--------|------|-------|--------|
| 6 | `feat/mc-06-hcl-parser` | PR 5 | HCL module parser (variables + outputs) | **DONE** |
| 7 | `feat/mc-07-callsite-tfvars` | PR 6 | HCL call site parser + tfvars loader | **DONE** |
| 8 | `feat/mc-08-evaluator` | PR 7 | Expression evaluator + function library via `pulumi/opentofu` | **DONE** |
| 9 | `feat/mc-09-state-population` | PR 8 | Component state population + auto-discovery + gap fixes | **IN PROGRESS** |
| 10 | `feat/mc-10-discovery-acceptance` | PR 9 | Comprehensive E2E state translation tests | TODO |

**Implementation notes (PRs 6-8):**
- `ParseModuleVariables` / `ParseModuleOutputs` / `ParseModuleCallSites` / `LoadTfvars` all in `pkg/hcl/parser.go`.
- Expression evaluator uses `opentofu/lang.Scope` to get the full Terraform function table (70+ functions tested).
- `CtyValueToPulumiPropertyValue` / `CtyMapToPulumiPropertyMap` / `PulumiPropertyMapToCtyMap` in `pkg/hcl/convert.go`.
- Call site parser filters meta-arguments (source, version, count, for_each, providers, depends_on).

**PR 9 scope change:** PR 9 now absorbs auto-discovery (originally PR 10) and all gap fixes identified during review. See updated PR 9 section below.

**PR 10 scope change:** PR 10 is now comprehensive E2E state translation tests (not clean preview tests). Tests validate translated Pulumi state values — not program generation or `pulumi preview`. See updated PR 10 section below.

---

## PR 1: Migration File Schema + Pure Functions ✅

### File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `pkg/migration/migration.go` | Add `Module` struct and `Modules` field to `Stack` |
| Create | `pkg/migration/migration_test.go` | Tests for module config loading |
| Create | `pkg/module_tree.go` | Type token derivation, name sanitization, module address parsing |
| Create | `pkg/module_tree_test.go` | Tests for pure functions |

### Task 1: Add `Module` struct to migration file format ✅

**Files:** Modify `pkg/migration/migration.go:56-80`

- [ ] **Step 1: Create test fixture — migration file with modules**

```json
// Save as pkg/migration/testdata/migration_with_modules.json
{
  "migration": {
    "tf-sources": "./tf",
    "pulumi-sources": "./pulumi",
    "stacks": [{
      "tf-state": "terraform.tfstate",
      "pulumi-stack": "dev",
      "modules": [
        {
          "tf-module": "module.vpc",
          "pulumi-type": "myproject:index:VpcComponent",
          "schema-path": "./schemas/vpc-component.json"
        },
        {
          "tf-module": "module.vpc.module.subnets",
          "pulumi-type": "myproject:network:SubnetGroup",
          "hcl-source": "./modules/subnets"
        }
      ],
      "resources": []
    }]
  }
}
```

- [ ] **Step 2: Write failing tests**

```go
// pkg/migration/migration_test.go
package migration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadMigrationWithModules(t *testing.T) {
	mf, err := LoadMigration("testdata/migration_with_modules.json")
	require.NoError(t, err)
	require.Len(t, mf.Migration.Stacks[0].Modules, 2)

	vpc := mf.Migration.Stacks[0].Modules[0]
	require.Equal(t, "module.vpc", vpc.TFModule)
	require.Equal(t, "myproject:index:VpcComponent", vpc.PulumiType)
	require.Equal(t, "./schemas/vpc-component.json", vpc.SchemaPath)
	require.Empty(t, vpc.HCLSource)

	subnets := mf.Migration.Stacks[0].Modules[1]
	require.Equal(t, "./modules/subnets", subnets.HCLSource)
}

func TestLoadMigrationWithoutModules_BackwardCompatible(t *testing.T) {
	// Use an existing test fixture or create a minimal one without modules
	mf, err := LoadMigration("testdata/migration_no_modules.json")
	require.NoError(t, err)
	require.Nil(t, mf.Migration.Stacks[0].Modules)
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./pkg/migration/ -run TestLoadMigration -v`

- [ ] **Step 4: Implement the struct changes**

In `pkg/migration/migration.go`, add `Module` struct after `Resource` (line 80):

```go
// Module represents a mapping between a Terraform module and a Pulumi component resource.
type Module struct {
	// Terraform module address such as "module.vpc" or "module.vpc.module.subnets".
	TFModule string `json:"tf-module"`

	// Pulumi type token for the component resource, e.g. "myproject:index:VpcComponent".
	// If empty, the type is auto-derived from the module name.
	PulumiType string `json:"pulumi-type,omitempty"`

	// Path to HCL source files for this module (Phase 2).
	HCLSource string `json:"hcl-source,omitempty"`

	// Path to Pulumi package schema JSON for component interface validation.
	// When provided, the schema is source of truth — parsed HCL interface is validated against it.
	SchemaPath string `json:"schema-path,omitempty"`
}
```

Add `Modules` field to `Stack` struct (after `Resources` field, line 65):

```go
	// Module mappings for component resource generation.
	Modules []Module `json:"modules,omitempty"`
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./pkg/migration/ -run TestLoadMigration -v`

- [ ] **Step 6: Commit**

```bash
git add pkg/migration/
git commit -m "feat: add Module struct and Modules field to migration file format"
```

---

### Task 2: Type token derivation and name sanitization ✅

**Files:** Create `pkg/module_tree.go`, `pkg/module_tree_test.go`

- [ ] **Step 1: Write failing tests**

```go
// pkg/module_tree_test.go
package pkg

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeriveComponentTypeToken(t *testing.T) {
	tests := []struct {
		moduleName string
		expected   string
	}{
		{"vpc", "terraform:module/vpc:Vpc"},
		{"s3_bucket", "terraform:module/s3Bucket:S3Bucket"},
		{"my_vpc_v2", "terraform:module/myVpcV2:MyVpcV2"},
		{"s3", "terraform:module/s3:S3"},
		{"VPC", "terraform:module/VPC:VPC"},
	}
	for _, tt := range tests {
		t.Run(tt.moduleName, func(t *testing.T) {
			result := deriveComponentTypeToken(tt.moduleName)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeModuleKey(t *testing.T) {
	tests := []struct {
		moduleName string
		key        string
		expected   string
	}{
		{"vpc", "0", "vpc-0"},
		{"vpc", "1", "vpc-1"},
		{"vpc", "us-east-1", "vpc-us-east-1"},
		{"vpc", "us_east_1", "vpc-us-east-1"},
		{"buckets", "logs", "buckets-logs"},
		{"vpc", "a--b", "vpc-a-b"},
	}
	for _, tt := range tests {
		t.Run(tt.moduleName+"_"+tt.key, func(t *testing.T) {
			result := sanitizeModuleInstanceName(tt.moduleName, tt.key)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestParseModuleSegments(t *testing.T) {
	tests := []struct {
		address  string
		expected []moduleSegment
	}{
		{
			"module.vpc.aws_subnet.this",
			[]moduleSegment{{name: "vpc"}},
		},
		{
			"module.vpc.module.subnets.aws_subnet.this",
			[]moduleSegment{{name: "vpc"}, {name: "subnets"}},
		},
		{
			"module.vpc[0].aws_subnet.this",
			[]moduleSegment{{name: "vpc", key: "0"}},
		},
		{
			`module.vpc["us-east-1"].aws_subnet.this`,
			[]moduleSegment{{name: "vpc", key: "us-east-1"}},
		},
		{
			`module.clusters[0].module.services["api"].aws_lambda_function.handler`,
			[]moduleSegment{{name: "clusters", key: "0"}, {name: "services", key: "api"}},
		},
		{
			"aws_s3_bucket.this",
			nil, // root-level, no modules
		},
	}
	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			result := parseModuleSegments(tt.address)
			require.Equal(t, tt.expected, result)
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/ -run "TestDeriveComponent|TestSanitize|TestParseModule" -v`

- [ ] **Step 3: Implement the functions**

Create `pkg/module_tree.go` with:
- `moduleSegment` struct (name, key fields)
- `deriveComponentTypeToken(moduleName string) string` — split on `_`, capitalize, join for PascalCase/camelCase
- `sanitizeModuleInstanceName(moduleName, key string) string` — replace non-alphanumeric with `-`, collapse, trim
- `parseModuleSegments(address string) []moduleSegment` — parse `module.X[Y].module.Z` patterns

See full implementation in spec review.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/ -run "TestDeriveComponent|TestSanitize|TestParseModule" -v`

- [ ] **Step 5: Commit**

```bash
git add pkg/module_tree.go pkg/module_tree_test.go
git commit -m "feat: add type token derivation, name sanitization, module address parsing"
```

### PR 1 Submission

```bash
git push -u origin feat/mc-01-migration-schema
gh pr create --title "feat(modules): migration schema + type token derivation" --body "$(cat <<'EOF'
## Summary
- Add `Module` struct to migration file format with `tf-module`, `pulumi-type`, `hcl-source` fields
- Pure functions: type token derivation, name sanitization, module address segment parsing
- Backward compatible — `modules` field is optional

## Test plan
- [ ] Migration file with/without modules loads correctly
- [ ] Type token derivation for various module name formats
- [ ] Name sanitization for indexed and string-keyed modules
- [ ] Module address parsing for simple, nested, indexed, and keyed modules

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## PR 2: Component Tree Builder ✅

### File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `pkg/module_tree.go` | `componentNode` struct, `buildComponentTree`, collision detection |
| Modify | `pkg/module_tree_test.go` | Tree builder tests |

### Task 3: Build component tree with collision detection ✅

**Files:** Modify `pkg/module_tree.go`, `pkg/module_tree_test.go`

- [ ] **Step 1: Write failing tests**

```go
// Add to pkg/module_tree_test.go

func TestBuildComponentTree_SingleModule(t *testing.T) {
	tree, err := buildComponentTree(
		[]string{"module.vpc.aws_subnet.this", "module.vpc.aws_route_table.rt"},
		nil,
	)
	require.NoError(t, err)
	require.Len(t, tree, 1)
	require.Equal(t, "vpc", tree[0].name)
	require.Equal(t, "terraform:module/vpc:Vpc", tree[0].typeToken)
	require.Nil(t, tree[0].children)
}

func TestBuildComponentTree_NestedModules(t *testing.T) {
	tree, err := buildComponentTree(
		[]string{
			"module.vpc.module.subnets.aws_subnet.this",
			"module.vpc.aws_vpc.main",
		},
		nil,
	)
	require.NoError(t, err)
	require.Len(t, tree, 1)
	require.Equal(t, "vpc", tree[0].name)
	require.Len(t, tree[0].children, 1)
	require.Equal(t, "subnets", tree[0].children[0].name)
}

func TestBuildComponentTree_WithTypeOverride(t *testing.T) {
	tree, err := buildComponentTree(
		[]string{"module.vpc.aws_subnet.this"},
		map[string]string{"module.vpc": "myproject:index:VpcComponent"},
	)
	require.NoError(t, err)
	require.Equal(t, "myproject:index:VpcComponent", tree[0].typeToken)
}

func TestBuildComponentTree_IndexedModules(t *testing.T) {
	tree, err := buildComponentTree(
		[]string{
			"module.vpc[0].aws_subnet.this",
			"module.vpc[1].aws_subnet.this",
		},
		nil,
	)
	require.NoError(t, err)
	require.Len(t, tree, 2)
	require.Equal(t, "vpc-0", tree[0].resourceName)
	require.Equal(t, "vpc-1", tree[1].resourceName)
	require.Equal(t, tree[0].typeToken, tree[1].typeToken)
}

func TestBuildComponentTree_SiblingsSortedAlphabetically(t *testing.T) {
	tree, err := buildComponentTree(
		[]string{
			"module.zebra.aws_s3_bucket.this",
			"module.alpha.aws_s3_bucket.this",
		},
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, "alpha", tree[0].resourceName)
	require.Equal(t, "zebra", tree[1].resourceName)
}

func TestBuildComponentTree_Empty(t *testing.T) {
	tree, err := buildComponentTree([]string{}, nil)
	require.NoError(t, err)
	require.Len(t, tree, 0)
}

func TestBuildComponentTree_RootResourcesIgnored(t *testing.T) {
	tree, err := buildComponentTree(
		[]string{"aws_s3_bucket.this"},
		nil,
	)
	require.NoError(t, err)
	require.Len(t, tree, 0)
}

func TestBuildComponentTree_SanitizationCollision(t *testing.T) {
	_, err := buildComponentTree(
		[]string{
			`module.vpc["us-east-1"].aws_subnet.this`,
			`module.vpc["us_east_1"].aws_subnet.that`,
		},
		nil,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "collision")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/ -run "TestBuildComponentTree" -v`

- [ ] **Step 3: Implement `componentNode` and `buildComponentTree`**

Add to `pkg/module_tree.go`:

```go
type componentNode struct {
	name         string           // module name (e.g., "vpc")
	key          string           // index/key if present
	resourceName string           // Pulumi resource name (e.g., "vpc" or "vpc-0")
	typeToken    string           // Pulumi type token
	modulePath   string           // full TF module path
	children     []*componentNode // child components
}

// buildComponentTree constructs a tree of component nodes from TF resource addresses.
// Returns error on name/type collisions. Results sorted alphabetically at each level.
func buildComponentTree(resourceAddresses []string, typeOverrides map[string]string) ([]*componentNode, error) {
	// 1. Parse all addresses, extract module paths, deduplicate
	// 2. Register all prefix paths for nesting
	// 3. Sort by depth (shorter first) for parent-before-child insertion
	// 4. Build tree: create nodes, link children to parents
	// 5. Validate: check for (type, name, parent) collisions
	// 6. Sort siblings alphabetically at each level
	// 7. Return root nodes
}
```

Also add helper functions:
- `buildModulePath(segments []moduleSegment) string` — e.g., `"module.vpc.module.subnets"`
- `buildModuleBasePath(segments []moduleSegment) string` — without indices, for override matching
- `toComponents(nodes []*componentNode, parentTypeChain string) []PulumiResource` — flatten tree to depth-first list
- `componentParentForResource(nodes []*componentNode, segments []moduleSegment) string` — look up parent type chain for a resource

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/ -run "TestBuildComponentTree" -v`

- [ ] **Step 5: Commit**

```bash
git add pkg/module_tree.go pkg/module_tree_test.go
git commit -m "feat: build component tree from TF resource addresses with collision detection"
```

### PR 2 Submission

```bash
git push -u origin feat/mc-02-component-tree
gh pr create --base feat/mc-01-migration-schema \
  --title "feat(modules): component tree builder with collision detection" \
  --body "..." # similar format
```

---

## PR 3: PulumiState Struct Changes + Component Insertion ✅

### File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `pkg/pulumi_state.go:31-61,147-211` | `Parent` field, `Components` field, `makeUrnWithParent`, component insertion |
| Modify | `pkg/pulumi_state_test.go` | Tests for component insertion |

### Task 4: Add `Parent` field, `Components`, and component insertion logic ✅

**Files:** Modify `pkg/pulumi_state.go`, `pkg/pulumi_state_test.go`

- [ ] **Step 1: Write failing tests**

```go
// Add to pkg/pulumi_state_test.go

func TestInsertResourcesIntoDeployment_WithComponents(t *testing.T) {
	stackName := "dev"
	projectName := "testproject"

	deployment := apitype.DeploymentV3{
		Resources: []apitype.ResourceV3{
			{
				URN:  makeUrn(stackName, projectName, "pulumi:pulumi:Stack", projectName+"-"+stackName),
				Type: "pulumi:pulumi:Stack",
			},
		},
	}

	providerID := PulumiResourceID{ID: "provider-uuid", Name: "default_6_0_0", Type: "pulumi:providers:aws"}
	state := &PulumiState{
		Providers: []PulumiResource{
			{PulumiResourceID: providerID, Inputs: resource.PropertyMap{}, Outputs: resource.PropertyMap{}},
		},
		Components: []PulumiResource{
			{
				PulumiResourceID: PulumiResourceID{Name: "vpc", Type: "terraform:module/vpc:Vpc"},
				Parent:           "", // top-level, parent is Stack
			},
		},
		Resources: []PulumiResource{
			{
				PulumiResourceID: PulumiResourceID{ID: "subnet-123", Name: "this", Type: "aws:ec2/subnet:Subnet"},
				Provider:         &providerID,
				Parent:           "terraform:module/vpc:Vpc", // parent type chain
				Inputs:           resource.PropertyMap{},
				Outputs:          resource.PropertyMap{},
			},
		},
	}

	result, err := InsertResourcesIntoDeployment(state, stackName, projectName, deployment)
	require.NoError(t, err)

	// Stack + provider + component + resource = 4
	require.Len(t, result.Resources, 4)

	// Verify ordering: Stack, provider, component, resource
	require.Equal(t, tokens.Type("pulumi:pulumi:Stack"), result.Resources[0].Type)
	require.True(t, result.Resources[1].Custom)  // provider
	require.False(t, result.Resources[2].Custom) // component
	require.True(t, result.Resources[3].Custom)  // resource

	// Verify component resource
	component := result.Resources[2]
	require.False(t, component.Custom)
	require.Equal(t, tokens.Type("terraform:module/vpc:Vpc"), component.Type)
	require.Empty(t, component.ID)
	require.Empty(t, component.Provider)

	// Verify resource is parented to component, not Stack
	res := result.Resources[3]
	require.Contains(t, string(res.Parent), "terraform:module/vpc:Vpc")
}

func TestInsertResourcesIntoDeployment_NestedComponents(t *testing.T) {
	stackName := "dev"
	projectName := "testproject"

	deployment := apitype.DeploymentV3{
		Resources: []apitype.ResourceV3{
			{
				URN:  makeUrn(stackName, projectName, "pulumi:pulumi:Stack", projectName+"-"+stackName),
				Type: "pulumi:pulumi:Stack",
			},
		},
	}

	providerID := PulumiResourceID{ID: "pid", Name: "default_1_0_0", Type: "pulumi:providers:aws"}
	state := &PulumiState{
		Providers: []PulumiResource{
			{PulumiResourceID: providerID, Inputs: resource.PropertyMap{}, Outputs: resource.PropertyMap{}},
		},
		Components: []PulumiResource{
			{
				PulumiResourceID: PulumiResourceID{Name: "vpc", Type: "terraform:module/vpc:Vpc"},
				Parent:           "", // parent is Stack
			},
			{
				PulumiResourceID: PulumiResourceID{Name: "subnets", Type: "terraform:module/subnets:Subnets"},
				Parent:           "terraform:module/vpc:Vpc", // parent is vpc component
			},
		},
		Resources: []PulumiResource{
			{
				PulumiResourceID: PulumiResourceID{ID: "subnet-1", Name: "this", Type: "aws:ec2/subnet:Subnet"},
				Provider:         &providerID,
				Parent:           "terraform:module/vpc:Vpc$terraform:module/subnets:Subnets",
				Inputs:           resource.PropertyMap{},
				Outputs:          resource.PropertyMap{},
			},
		},
	}

	result, err := InsertResourcesIntoDeployment(state, stackName, projectName, deployment)
	require.NoError(t, err)
	require.Len(t, result.Resources, 5) // Stack + provider + 2 components + resource

	// subnets component should be parented to vpc component
	subnets := result.Resources[3]
	require.False(t, subnets.Custom)
	require.Contains(t, string(subnets.Parent), "terraform:module/vpc:Vpc")

	// resource should be parented to subnets component
	res := result.Resources[4]
	require.Contains(t, string(res.Parent), "terraform:module/subnets:Subnets")

	// URN should encode parent type chain with $ delimiter
	require.Contains(t, string(res.URN), "terraform:module/vpc:Vpc$terraform:module/subnets:Subnets$aws:ec2/subnet:Subnet")
}

func TestInsertResourcesIntoDeployment_NoComponents_BackwardCompat(t *testing.T) {
	// Existing behavior: empty Components slice = no change
	stackName := "dev"
	projectName := "testproject"

	deployment := apitype.DeploymentV3{
		Resources: []apitype.ResourceV3{
			{
				URN:  makeUrn(stackName, projectName, "pulumi:pulumi:Stack", projectName+"-"+stackName),
				Type: "pulumi:pulumi:Stack",
			},
		},
	}

	providerID := PulumiResourceID{ID: "pid", Name: "default_1_0_0", Type: "pulumi:providers:random"}
	state := &PulumiState{
		Providers: []PulumiResource{
			{PulumiResourceID: providerID, Inputs: resource.PropertyMap{}, Outputs: resource.PropertyMap{}},
		},
		Components: nil, // no components
		Resources: []PulumiResource{
			{
				PulumiResourceID: PulumiResourceID{ID: "abc", Name: "test", Type: "random:index/randomPet:RandomPet"},
				Provider:         &providerID,
				Inputs:           resource.PropertyMap{},
				Outputs:          resource.PropertyMap{},
			},
		},
	}

	result, err := InsertResourcesIntoDeployment(state, stackName, projectName, deployment)
	require.NoError(t, err)
	require.Len(t, result.Resources, 3) // Stack + provider + resource, no components
	// Resource parent should be Stack
	require.Contains(t, string(result.Resources[2].Parent), "pulumi:pulumi:Stack")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/ -run "TestInsertResourcesIntoDeployment_WithComponents|TestInsertResourcesIntoDeployment_Nested|TestInsertResourcesIntoDeployment_NoComponents_Backward" -v`

- [ ] **Step 3: Implement struct changes**

Update `PulumiResource` in `pkg/pulumi_state.go` (line 46):

```go
type PulumiResource struct {
	PulumiResourceID

	Inputs  resource.PropertyMap
	Outputs resource.PropertyMap

	// For resources this identifies the associated provider.
	// For provider resources and components this is nil.
	Provider *PulumiResourceID

	// Parent type chain for URN encoding (e.g., "terraform:module/vpc:Vpc" or
	// "terraform:module/vpc:Vpc$terraform:module/subnets:Subnets").
	// Empty string means parent is Stack.
	Parent string
}
```

Update `PulumiState` (line 58):

```go
type PulumiState struct {
	Providers  []PulumiResource
	Components []PulumiResource
	Resources  []PulumiResource
}
```

Add `makeUrnWithParent` (after `makeUrn`, line 33):

```go
func makeUrnWithParent(stackName, projectName, parentTypeChain, typeName, resourceName string) resource.URN {
	fullType := typeName
	if parentTypeChain != "" {
		fullType = parentTypeChain + "$" + typeName
	}
	return resource.URN(fmt.Sprintf("urn:pulumi:%s::%s::%s::%s", stackName, projectName, fullType, resourceName))
}
```

- [ ] **Step 4: Update `InsertResourcesIntoDeployment`**

Add component insertion between providers and resources. Use a `componentURNs` map for O(1) parent lookups:

```go
// After provider loop:
componentURNs := map[string]resource.URN{} // type chain -> URN

for _, comp := range state.Components {
	urn := makeUrnWithParent(stackName, projectName, comp.Parent, comp.Type, comp.Name)

	parentURN := resource.URN(stackResource.URN)
	if comp.Parent != "" {
		if parentComponentURN, ok := componentURNs[comp.Parent]; ok {
			parentURN = parentComponentURN
		}
	}

	fullTypeChain := comp.Type
	if comp.Parent != "" {
		fullTypeChain = comp.Parent + "$" + comp.Type
	}
	componentURNs[fullTypeChain] = urn

	inputs := comp.Inputs
	if inputs == nil {
		inputs = resource.PropertyMap{}
	}
	outputs := comp.Outputs
	if outputs == nil {
		outputs = resource.PropertyMap{}
	}

	deployment.Resources = append(deployment.Resources, apitype.ResourceV3{
		URN:      urn,
		Custom:   false,
		Type:     tokens.Type(comp.Type),
		Inputs:   inputs.Mappable(),
		Outputs:  outputs.Mappable(),
		Parent:   parentURN,
		Created:  &now,
		Modified: &now,
	})
}

// Update resource loop to use parent type chain:
for _, res := range state.Resources {
	// ... existing provider link code ...

	urn := makeUrnWithParent(stackName, projectName, res.Parent, res.Type, res.Name)
	parentURN := resource.URN(stackResource.URN)
	if res.Parent != "" {
		if parentComponentURN, ok := componentURNs[res.Parent]; ok {
			parentURN = parentComponentURN
		}
	}

	deployment.Resources = append(deployment.Resources, apitype.ResourceV3{
		URN:      urn,
		Custom:   true,
		// ... rest unchanged, but use parentURN instead of stackResource.URN ...
	})
}
```

- [ ] **Step 5: Run new and existing tests**

Run: `go test ./pkg/ -run "TestInsertResourcesIntoDeployment" -v`
Expected: All tests PASS (new component tests + existing tests via backward compat).

- [ ] **Step 6: Commit**

```bash
git add pkg/pulumi_state.go pkg/pulumi_state_test.go
git commit -m "feat: add Parent field, Components, and component insertion to PulumiState"
```

### PR 3 Submission

```bash
git push -u origin feat/mc-03-pulumi-state
gh pr create --base feat/mc-02-component-tree \
  --title "feat(modules): PulumiState struct changes + component insertion" \
  --body "..." # similar format
```

---

## PR 4: Pipeline Integration + CLI Flags ✅

### File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `pkg/state_adapter.go:50-112,120-151,160-221,300-316` | Thread `enableComponents`/`typeOverrides` through call chain, update `PulumiNameFromTerraformAddress` |
| Modify | `pkg/state_adapter_test.go` | Update existing module tests, add backward compat test |
| Modify | `cmd/stack.go` | Add `--module-type-map` and `--no-module-components` flags |

### Task 5: Integrate module tree into `convertState` and update naming ✅

**Files:** Modify `pkg/state_adapter.go`, `pkg/state_adapter_test.go`

- [ ] **Step 1: Update `PulumiNameFromTerraformAddress` with short name support**

Add `useShortName bool` parameter. When true, return only the resource name part (not module path). Test with indexed module addresses too.

```go
func TestPulumiNameFromTerraformAddress_ShortName(t *testing.T) {
	tests := []struct {
		address      string
		resourceType string
		useShortName bool
		expected     string
	}{
		{"module.vpc.aws_subnet.this", "aws_subnet", true, "this"},
		{"module.vpc.aws_subnet.this", "aws_subnet", false, "vpc_this"},
		{"module.vpc[0].aws_subnet.this", "aws_subnet", true, "this"},
		{"module.vpc.module.subnets.aws_subnet.this", "aws_subnet", true, "this"},
		{"aws_s3_bucket.mybucket", "aws_s3_bucket", true, "mybucket"},
		{"aws_s3_bucket.mybucket", "aws_s3_bucket", false, "mybucket"},
	}
	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			result := PulumiNameFromTerraformAddress(tt.address, tt.resourceType, tt.useShortName)
			require.Equal(t, tt.expected, result)
		})
	}
}
```

- [ ] **Step 2: Update `convertState` signature and callers**

Thread new parameters through the full call chain:

```
TranslateAndWriteState(ctx, from, to, out, plugins, strict, enableComponents, typeOverrides)
  → TranslateState(ctx, tfState, providerVersions, pulumiDir, enableComponents, typeOverrides)
    → convertState(tfState, pulumiProviders, enableComponents, typeOverrides)
```

Update `translateStateFromJson` test helper to pass `enableComponents: true, typeOverrides: nil`.

- [ ] **Step 3: Add component tree integration in `convertState`**

```go
// First pass: collect resource addresses
var resourceAddresses []string
if enableComponents {
	tofu.VisitResources(tfState, func(r *tfjson.StateResource) error {
		resourceAddresses = append(resourceAddresses, r.Address)
		return nil
	}, &tofu.VisitOptions{})
}

// Build component tree
var componentTree []*componentNode
if enableComponents && len(resourceAddresses) > 0 {
	var err error
	componentTree, err = buildComponentTree(resourceAddresses, typeOverrides)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build component tree: %w", err)
	}
	pulumiState.Components = toComponents(componentTree, "")
}

// In resource visitor: set parent and use short name
if enableComponents {
	pulumiResource.Name = PulumiNameFromTerraformAddress(resource.Address, resource.Type, true)
	segments := parseModuleSegments(resource.Address)
	pulumiResource.Parent = componentParentForResource(componentTree, segments)
}
```

- [ ] **Step 4: Update existing module tests**

`TestConvertTwoModules` and `TestConvertNestedModules` now produce component resources. Update assertions to expect them:

```go
func TestConvertTwoModules(t *testing.T) {
	// ... existing setup ...
	var components []apitype.ResourceV3
	for _, r := range data.Export.Deployment.Resources {
		if !r.Custom && string(r.Type) != "pulumi:pulumi:Stack" {
			components = append(components, r)
		}
	}
	require.Len(t, components, 2) // two module components

	// Existing URN uniqueness check still passes
	bucketURNs := make(map[string]bool)
	// ...
}
```

- [ ] **Step 5: Add backward compat test**

```go
func TestConvertTwoModules_FlatMode(t *testing.T) {
	ctx := context.Background()
	stackFolder := createPulumiStack(t)
	// Call with enableComponents=false
	// Assert: no component resources, names include module path, parent=Stack
}
```

- [ ] **Step 6: Run full test suite**

Run: `go test ./pkg/ -v -timeout 120s`

- [ ] **Step 7: Commit**

```bash
git add pkg/state_adapter.go pkg/state_adapter_test.go
git commit -m "feat: integrate module tree into convertState pipeline"
```

### Task 6: Add CLI flags ✅

**Files:** Modify `cmd/stack.go`

- [ ] **Step 1: Add flags**

```go
var noModuleComponents bool
var moduleTypeMaps []string

// In RunE:
typeOverrides := map[string]string{}
for _, mapping := range moduleTypeMaps {
	parts := strings.SplitN(mapping, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid --module-type-map format %q, expected module.name=type:token", mapping)
	}
	typeOverrides[parts[0]] = parts[1]
}

if noModuleComponents && len(moduleTypeMaps) > 0 {
	fmt.Fprintf(os.Stderr, "Warning: --module-type-map is ignored when --no-module-components is set\n")
	typeOverrides = nil
}

err := pkg.TranslateAndWriteState(cmd.Context(), from, to, out, plugins, strict,
	!noModuleComponents, typeOverrides)

// Flag registration:
cmd.Flags().BoolVar(&noModuleComponents, "no-module-components", false,
	"Disable creation of component resources for Terraform modules (flat mode)")
cmd.Flags().StringArrayVar(&moduleTypeMaps, "module-type-map", nil,
	"Override component type token for a module (repeatable, format: module.name=pkg:mod:Type)")
```

- [ ] **Step 2: Run full test suite**

Run: `go test ./... -timeout 120s`

- [ ] **Step 3: Commit**

```bash
git add cmd/stack.go
git commit -m "feat: add --module-type-map and --no-module-components CLI flags"
```

### PR 4 Submission

```bash
git push -u origin feat/mc-04-pipeline-integration
gh pr create --base feat/mc-03-pulumi-state \
  --title "feat(modules): pipeline integration + CLI flags" \
  --body "..." # similar format
```

---

## PR 4.5: Schema Validation Support ✅

### File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `pkg/migration/migration.go` | Add `SchemaPath` field to `Module` struct |
| Create | `pkg/component_schema.go` | Load schema, extract component interface, validate against parsed HCL |
| Create | `pkg/component_schema_test.go` | Tests |
| Create | `pkg/testdata/schemas/vpc_component_schema.json` | Test fixture: valid Pulumi package schema |
| Modify | `cmd/stack.go` | Add `--module-schema` CLI flag |

### Task 6.5: Schema validation for component interfaces

**Files:** `pkg/component_schema.go`, `pkg/component_schema_test.go`, `pkg/migration/migration.go`, `cmd/stack.go`

Uses `github.com/pulumi/pulumi/pkg/v3/codegen/schema` (already in `go.mod` v3.222.0). No new dependencies.

**Note on testing:** PR 4.5 unit tests use a hand-crafted schema fixture to test `LoadComponentSchema` and `ValidateAgainstSchema` in isolation. End-to-end schema validation tests (with real schema from `pulumi package get-schema` on a component provider, validated against HCL-parsed inputs/outputs) belong in **PR 9** — that's when HCL parsing produces the parsed interface needed to validate against the schema. Type compatibility is handled by the value conversion pipeline (`cty.Value` → `resource.PropertyMap` via `tfbridge`), not by schema validation — validation only checks field presence and required fields.

- [ ] **Step 1: Create test fixture — minimal Pulumi package schema JSON**

```json
// Save as pkg/testdata/schemas/vpc_component_schema.json
{
  "name": "myproject",
  "version": "0.1.0",
  "resources": {
    "myproject:index:VpcComponent": {
      "isComponent": true,
      "inputProperties": {
        "cidr": {
          "type": "string"
        },
        "name": {
          "type": "string"
        }
      },
      "requiredInputs": ["cidr", "name"],
      "properties": {
        "vpcId": {
          "type": "string"
        }
      },
      "required": ["vpcId"]
    }
  }
}
```

- [ ] **Step 2: Write failing tests**

```go
// pkg/component_schema_test.go
package pkg

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadComponentSchema(t *testing.T) {
	iface, err := LoadComponentSchema("testdata/schemas/vpc_component_schema.json", "myproject:index:VpcComponent")
	require.NoError(t, err)
	require.Len(t, iface.Inputs, 2)
	require.Len(t, iface.Outputs, 1)

	// Check input fields
	cidr := findField(iface.Inputs, "cidr")
	require.NotNil(t, cidr)
	require.Equal(t, "string", cidr.Type)
	require.True(t, cidr.Required)

	name := findField(iface.Inputs, "name")
	require.NotNil(t, name)
	require.True(t, name.Required)

	// Check output fields
	vpcId := findField(iface.Outputs, "vpcId")
	require.NotNil(t, vpcId)
	require.Equal(t, "string", vpcId.Type)
}

func TestLoadComponentSchema_NotFound(t *testing.T) {
	_, err := LoadComponentSchema("testdata/schemas/vpc_component_schema.json", "myproject:index:DoesNotExist")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestLoadComponentSchema_NotComponent(t *testing.T) {
	// Test with a resource that is not a component (isComponent = false)
	// Should error with descriptive message
}

func TestValidateSchemaMatch(t *testing.T) {
	schema := &ComponentInterface{
		Inputs: []ComponentField{
			{Name: "cidr", Type: "string", Required: true},
			{Name: "name", Type: "string", Required: true},
		},
		Outputs: []ComponentField{
			{Name: "vpcId", Type: "string"},
		},
	}
	parsed := &ComponentInterface{
		Inputs: []ComponentField{
			{Name: "cidr", Type: "string"},
			{Name: "name", Type: "string"},
		},
		Outputs: []ComponentField{
			{Name: "vpcId", Type: "string"},
		},
	}
	err := ValidateAgainstSchema(parsed, schema)
	require.NoError(t, err)
}

func TestValidateSchemaMatch_MissingInput(t *testing.T) {
	schema := &ComponentInterface{
		Inputs: []ComponentField{
			{Name: "cidr", Type: "string", Required: true},
			{Name: "name", Type: "string", Required: true},
		},
	}
	parsed := &ComponentInterface{
		Inputs: []ComponentField{
			{Name: "cidr", Type: "string"},
			// "name" is missing — schema requires it
		},
	}
	err := ValidateAgainstSchema(parsed, schema)
	require.Error(t, err)
	require.Contains(t, err.Error(), "name")
	require.Contains(t, err.Error(), "required")
}

func TestValidateSchemaMatch_ExtraOutput(t *testing.T) {
	schema := &ComponentInterface{
		Outputs: []ComponentField{
			{Name: "vpcId", Type: "string"},
		},
	}
	parsed := &ComponentInterface{
		Outputs: []ComponentField{
			{Name: "vpcId", Type: "string"},
			{Name: "extraField", Type: "string"}, // not in schema
		},
	}
	err := ValidateAgainstSchema(parsed, schema)
	require.Error(t, err)
	require.Contains(t, err.Error(), "extraField")
	require.Contains(t, err.Error(), "not in schema")
}

// helpers
func findField(fields []ComponentField, name string) *ComponentField {
	for i := range fields {
		if fields[i].Name == name {
			return &fields[i]
		}
	}
	return nil
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./pkg/ -run "TestLoadComponentSchema|TestValidateSchema" -v`

- [ ] **Step 4: Implement**

```go
// pkg/component_schema.go
package pkg

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pulumi/pulumi/pkg/v3/codegen/schema"
)

// ComponentField represents a single input or output field of a component.
type ComponentField struct {
	Name     string
	Type     string
	Required bool
}

// ComponentInterface represents the inputs and outputs of a component resource.
type ComponentInterface struct {
	Inputs  []ComponentField
	Outputs []ComponentField
}

// LoadComponentSchema loads a Pulumi package schema JSON file and extracts the
// component interface (inputs and outputs) for the given component type token.
func LoadComponentSchema(schemaPath string, componentType string) (*ComponentInterface, error) {
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("reading schema file: %w", err)
	}

	var spec schema.PackageSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parsing schema JSON: %w", err)
	}

	pkg, err := schema.ImportSpec(spec, nil, schema.ValidationOptions{})
	if err != nil {
		return nil, fmt.Errorf("importing schema spec: %w", err)
	}

	resource, ok := pkg.GetResource(componentType)
	if !ok {
		return nil, fmt.Errorf("component type %q not found in schema", componentType)
	}
	if !resource.IsComponent {
		return nil, fmt.Errorf("resource %q is not a component (isComponent=false)", componentType)
	}

	iface := &ComponentInterface{}

	// Extract inputs
	requiredInputs := map[string]bool{}
	for _, name := range resource.RequiredInputs {
		requiredInputs[name] = true
	}
	for _, prop := range resource.InputProperties {
		iface.Inputs = append(iface.Inputs, ComponentField{
			Name:     prop.Name,
			Type:     prop.Type.String(),
			Required: requiredInputs[prop.Name],
		})
	}

	// Extract outputs
	for _, prop := range resource.Properties {
		iface.Outputs = append(iface.Outputs, ComponentField{
			Name: prop.Name,
			Type: prop.Type.String(),
		})
	}

	return iface, nil
}

// ValidateAgainstSchema validates that a parsed component interface matches a schema.
// Schema is source of truth — mismatch is an error.
func ValidateAgainstSchema(parsed *ComponentInterface, schema *ComponentInterface) error {
	// Check required inputs are present
	parsedInputs := map[string]bool{}
	for _, f := range parsed.Inputs {
		parsedInputs[f.Name] = true
	}
	for _, f := range schema.Inputs {
		if f.Required && !parsedInputs[f.Name] {
			return fmt.Errorf("input %q is required by schema but not found in parsed interface", f.Name)
		}
	}

	// Check parsed outputs are in schema
	schemaOutputs := map[string]bool{}
	for _, f := range schema.Outputs {
		schemaOutputs[f.Name] = true
	}
	for _, f := range parsed.Outputs {
		if !schemaOutputs[f.Name] {
			return fmt.Errorf("output %q not in schema", f.Name)
		}
	}

	return nil
}
```

- [ ] **Step 5: Add `SchemaPath` to `Module` struct**

In `pkg/migration/migration.go`, add to `Module` struct:

```go
	// Path to Pulumi package schema JSON for validation.
	SchemaPath string `json:"schema-path,omitempty"`
```

- [ ] **Step 6: Add `--module-schema` CLI flag**

In `cmd/stack.go`:

```go
var moduleSchemas []string
cmd.Flags().StringArrayVar(&moduleSchemas, "module-schema", nil,
	"Pulumi package schema for component validation (repeatable, format: module.name=./path/to/schema.json)")
```

Parse and pass through the call chain, same pattern as `--module-type-map`.

- [ ] **Step 7: Wire into pipeline**

In `convertState`, after HCL parsing produces inputs/outputs, if schema is provided:

```go
if schemaPath != "" {
	schemaIface, err := LoadComponentSchema(schemaPath, componentTypeToken)
	if err != nil {
		return nil, nil, fmt.Errorf("loading schema for %s: %w", modulePath, err)
	}
	if err := ValidateAgainstSchema(parsedIface, schemaIface); err != nil {
		return nil, nil, fmt.Errorf("schema validation failed for %s: %w", modulePath, err)
	}
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./pkg/ -run "TestLoadComponentSchema|TestValidateSchema" -v`

- [ ] **Step 9: Run full test suite**

Run: `go test ./... -timeout 120s`

- [ ] **Step 10: Commit**

```bash
git add pkg/component_schema.go pkg/component_schema_test.go pkg/testdata/schemas/ pkg/migration/migration.go cmd/stack.go
git commit -m "feat: add schema validation for component interfaces"
```

### PR 4.5 Submission

```bash
git push -u origin feat/mc-04b-schema-validation
gh pr create --base feat/mc-04-pipeline-integration \
  --title "feat(modules): schema validation for component interfaces" \
  --body "$(cat <<'EOF'
## Summary
- Load Pulumi package schema JSON and extract component interface (inputs/outputs)
- Validate parsed HCL interface against schema — schema is source of truth
- Mismatch (missing required input, extra output) = descriptive error
- `--module-schema` CLI flag and `schema-path` migration file field
- Uses existing `pulumi/pulumi/pkg/v3/codegen/schema` — no new dependencies

## Test plan
- [ ] Load schema and extract component inputs/outputs
- [ ] Schema match passes validation
- [ ] Missing required input fails with descriptive error
- [ ] Extra output not in schema fails with descriptive error
- [ ] Component type not found in schema → error
- [ ] Migration file with `schema-path` loads correctly
- [ ] Full test suite passes

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## PR 5: Real Deployment Test Fixtures + Integration Tests ✅

### File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `pkg/testdata/tofu_state_indexed_modules.json` | Test fixture from real `count`-based module deployment |
| Create | `pkg/testdata/tofu_state_keyed_modules.json` | Test fixture from real `for_each` string-keyed module deployment |
| Modify | `pkg/state_adapter_test.go` | Tests using real fixtures |
| Modify | `test/translate_test.go` | End-to-end integration test |

### Task 7: Generate test fixtures from real deployments

- [ ] **Step 1: Create Terraform HCL for indexed modules**

```hcl
# /tmp/tf-indexed-test/main.tf
module "pet" {
  count  = 2
  source = "./modules/pet"
  prefix = "test-${count.index}"
}

# /tmp/tf-indexed-test/modules/pet/main.tf
variable "prefix" { type = string }
resource "random_pet" "this" {
  prefix = var.prefix
}
output "name" { value = random_pet.this.id }
```

- [ ] **Step 2: Deploy and capture TF state**

```bash
cd /tmp/tf-indexed-test && tofu init && tofu apply -auto-approve
tofu show -json > pkg/testdata/tofu_state_indexed_modules.json
tofu destroy -auto-approve
```

- [ ] **Step 3: Create equivalent Pulumi component code, deploy, capture state**

Write a Pulumi Go program with `ComponentResource` for the pet module, deploy with `pulumi up`, export state as expected output.

- [ ] **Step 4: Repeat for `for_each` keyed modules**

Similar process with `for_each = toset(["alpha", "beta"])`.

- [ ] **Step 5: Write tests using real fixtures**

```go
func TestConvertIndexedModules(t *testing.T) {
	ctx := context.Background()
	stackFolder := createPulumiStack(t)
	data, err := translateStateFromJson(ctx, "testdata/tofu_state_indexed_modules.json", stackFolder)
	require.NoError(t, err)

	var components []apitype.ResourceV3
	for _, r := range data.Export.Deployment.Resources {
		if !r.Custom && string(r.Type) != "pulumi:pulumi:Stack" {
			components = append(components, r)
		}
	}
	require.Len(t, components, 2) // pet-0, pet-1
	require.Equal(t, components[0].Type, components[1].Type) // same type token
	require.NotEqual(t, string(components[0].URN), string(components[1].URN))
}

func TestConvertKeyedModules(t *testing.T) {
	// Similar test with for_each fixture
}
```

- [ ] **Step 6: Add end-to-end integration test**

```go
// In test/translate_test.go
func TestTranslateModulesEndToEnd(t *testing.T) {
	// Full pipeline: TF state with modules → translated Pulumi state → pulumi stack import succeeds
}
```

- [ ] **Step 7: Commit**

```bash
git add pkg/testdata/ pkg/state_adapter_test.go test/
git commit -m "test: add real deployment test fixtures for indexed and keyed modules"
```

### PR 5 Submission

```bash
git push -u origin feat/mc-05-integration-tests
gh pr create --base feat/mc-04-pipeline-integration \
  --title "test(modules): real deployment fixtures + integration tests" \
  --body "..." # similar format
```

---

## PR 6: HCL Module Parser ✅

### File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `pkg/hcl/parser.go` | Parse module `variable` and `output` blocks |
| Create | `pkg/hcl/parser_test.go` | Tests |
| Create | `pkg/hcl/testdata/simple_module/` | Test HCL fixture |

### Task 8: Parse module variables and outputs

- [ ] **Step 1: Create test fixture**

```hcl
# pkg/hcl/testdata/simple_module/variables.tf
variable "cidr" {
  type        = string
  description = "The CIDR block"
}

variable "name" {
  type    = string
  default = "default-vpc"
}

variable "enable_dns" {
  type    = bool
  default = true
}

variable "tags" {
  type    = map(string)
  default = {}
}

# pkg/hcl/testdata/simple_module/outputs.tf
output "vpc_id" {
  value       = aws_vpc.this.id
  description = "The VPC ID"
}

output "cidr_block" {
  value = aws_vpc.this.cidr_block
}

# pkg/hcl/testdata/simple_module/main.tf
resource "aws_vpc" "this" {
  cidr_block         = var.cidr
  enable_dns_support = var.enable_dns
  tags               = var.tags
}
```

- [ ] **Step 2: Write failing tests**

```go
// pkg/hcl/parser_test.go
package hcl

import (
	"testing"
	"github.com/stretchr/testify/require"
)

func TestParseModuleVariables(t *testing.T) {
	vars, err := ParseModuleVariables("testdata/simple_module")
	require.NoError(t, err)
	require.Len(t, vars, 4)

	cidr := findVar(vars, "cidr")
	require.NotNil(t, cidr)
	require.Equal(t, "string", cidr.Type)
	require.Nil(t, cidr.Default)
	require.Equal(t, "The CIDR block", cidr.Description)

	name := findVar(vars, "name")
	require.NotNil(t, name)
	require.Equal(t, "default-vpc", name.Default.AsString())

	tags := findVar(vars, "tags")
	require.NotNil(t, tags)
	require.Equal(t, "map(string)", tags.Type)
}

func TestParseModuleOutputs(t *testing.T) {
	outputs, err := ParseModuleOutputs("testdata/simple_module")
	require.NoError(t, err)
	require.Len(t, outputs, 2)

	vpcID := findOutput(outputs, "vpc_id")
	require.NotNil(t, vpcID)
	require.Equal(t, "The VPC ID", vpcID.Description)
	require.NotNil(t, vpcID.Expression) // raw HCL expression preserved
}

func TestParseModuleVariables_EmptyDir(t *testing.T) {
	vars, err := ParseModuleVariables("testdata/nonexistent")
	require.Error(t, err)
}

// helpers
func findVar(vars []ModuleVariable, name string) *ModuleVariable { /* ... */ }
func findOutput(outputs []ModuleOutput, name string) *ModuleOutput { /* ... */ }
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./pkg/hcl/ -run "TestParseModule" -v`

- [ ] **Step 4: Implement the parser**

```go
// pkg/hcl/parser.go
package hcl

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
)

type ModuleVariable struct {
	Name        string
	Type        string    // HCL type string (e.g., "string", "map(string)")
	Default     *cty.Value // default value, nil if required
	Description string
}

type ModuleOutput struct {
	Name        string
	Description string
	Expression  hcl.Expression // raw HCL expression for later evaluation
}

func ParseModuleVariables(moduleDir string) ([]ModuleVariable, error) {
	// 1. Glob for *.tf files in moduleDir
	// 2. Parse each with hclparse.NewParser()
	// 3. Extract "variable" blocks
	// 4. For each: read name, type, default, description attributes
}

func ParseModuleOutputs(moduleDir string) ([]ModuleOutput, error) {
	// 1. Glob for *.tf files in moduleDir
	// 2. Parse each with hclparse.NewParser()
	// 3. Extract "output" blocks
	// 4. For each: read name, description; preserve value Expression for later evaluation
}
```

- [ ] **Step 5: Run tests to verify they pass**

- [ ] **Step 6: Commit**

```bash
git add pkg/hcl/
git commit -m "feat: add HCL module parser for variable and output blocks"
```

### PR 6 Submission

```bash
git push -u origin feat/mc-06-hcl-parser
gh pr create --base feat/mc-05-integration-tests \
  --title "feat(modules): HCL module parser for variables and outputs" \
  --body "..." # similar format
```

---

## PR 7: Call Site Parser + Tfvars Loader ✅

### File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `pkg/hcl/parser.go` | `ParseModuleCallSites` function |
| Modify | `pkg/hcl/parser_test.go` | Call site tests |
| Create | `pkg/hcl/testdata/root_module/` | Root module test fixture |
| Create | `pkg/hcl/testdata/tfvars/` | Tfvars test fixture |
| Modify | `pkg/hcl/evaluator.go` | `LoadTfvars` function |

### Task 9: Parse module call sites

- [ ] **Step 1: Create test fixture**

```hcl
# pkg/hcl/testdata/root_module/main.tf
module "vpc" {
  source     = "../simple_module"
  cidr       = "10.0.0.0/16"
  name       = "production"
  enable_dns = true
}

module "vpc_staging" {
  source     = "../simple_module"
  cidr       = var.staging_cidr
  name       = "staging"
}

variable "staging_cidr" {
  type    = string
  default = "10.1.0.0/16"
}
```

- [ ] **Step 2: Write failing tests**

```go
func TestParseModuleCallSites(t *testing.T) {
	calls, err := ParseModuleCallSites("testdata/root_module")
	require.NoError(t, err)
	require.Len(t, calls, 2)

	vpc := findCall(calls, "vpc")
	require.NotNil(t, vpc)
	require.Equal(t, "../simple_module", vpc.Source)
	require.Len(t, vpc.Arguments, 3)
}
```

- [ ] **Step 3: Implement**

```go
type ModuleCallSite struct {
	Name      string
	Source    string
	Arguments map[string]hcl.Expression
}

func ParseModuleCallSites(rootDir string) ([]ModuleCallSite, error) {
	// Parse all .tf files, extract "module" blocks
	// For each: read name, source attribute, remaining attributes as argument expressions
}
```

- [ ] **Step 4: Run tests, commit**

### Task 10: Load terraform.tfvars

- [ ] **Step 1: Create test fixture**

```hcl
# pkg/hcl/testdata/tfvars/terraform.tfvars
cidr       = "10.0.0.0/16"
name       = "production"
enable_dns = true
tags       = { Environment = "prod", Team = "infra" }
```

- [ ] **Step 2: Write failing tests**

```go
func TestLoadTfvars(t *testing.T) {
	vars, err := LoadTfvars("testdata/tfvars/terraform.tfvars")
	require.NoError(t, err)
	require.Equal(t, "10.0.0.0/16", vars["cidr"].AsString())
	require.Equal(t, cty.True, vars["enable_dns"])
}

func TestLoadTfvars_NotFound(t *testing.T) {
	vars, err := LoadTfvars("nonexistent/terraform.tfvars")
	require.NoError(t, err) // not an error, just empty
	require.Len(t, vars, 0)
}
```

- [ ] **Step 3: Implement**

```go
func LoadTfvars(path string) (map[string]cty.Value, error) {
	// Use hclparse to read .tfvars file (HCL native syntax)
	// Extract all top-level attributes as cty.Value
	// Return empty map if file doesn't exist
}
```

- [ ] **Step 4: Run tests, commit**

### PR 7 Submission

```bash
git push -u origin feat/mc-07-callsite-tfvars
gh pr create --base feat/mc-06-hcl-parser \
  --title "feat(modules): call site parser + tfvars loader" \
  --body "..." # similar format
```

---

## PR 8: Expression Evaluator ✅

### File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `pkg/hcl/evaluator.go` | `EvalContext`, `buildFunctionTable`, expression evaluation |
| Create | `pkg/hcl/evaluator_test.go` | Tests covering all expression types |

### Task 11: Implement HCL expression evaluator

**Verified:** Both function libraries are importable (no submodule needed):
- `github.com/pulumi/opentofu/lang/funcs` — 60+ Terraform-specific functions (cidr, template, crypto, time, etc.)
- `github.com/zclconf/go-cty/cty/function/stdlib` — 80+ standard functions (join, split, regex, json, collections, etc.)

HCL's evaluator natively handles literals, variable refs, conditionals, and `for` expressions without any function library. The function table is only needed for function calls like `join(...)` or `cidrsubnets(...)`.

- [ ] **Step 1: Write failing tests**

Test helpers parse HCL expression strings via `hclsyntax.ParseExpression`:

```go
// pkg/hcl/evaluator_test.go
func parseExpr(t *testing.T, src string) hcl.Expression {
	expr, diags := hclsyntax.ParseExpression([]byte(src), "test.hcl", hcl.Pos{})
	require.False(t, diags.HasErrors(), diags.Error())
	return expr
}

func TestEvaluateLiteral(t *testing.T) {
	ctx := NewEvalContext(nil, nil, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, `"10.0.0.0/16"`))
	require.NoError(t, err)
	require.Equal(t, "10.0.0.0/16", val.AsString())
}

func TestEvaluateVariableRef(t *testing.T) {
	vars := map[string]cty.Value{"cidr": cty.StringVal("10.0.0.0/16")}
	ctx := NewEvalContext(vars, nil, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, "var.cidr"))
	require.NoError(t, err)
	require.Equal(t, "10.0.0.0/16", val.AsString())
}

func TestEvaluateResourceRef(t *testing.T) {
	resources := map[string]map[string]cty.Value{
		"aws_vpc": {"this": cty.ObjectVal(map[string]cty.Value{
			"id":         cty.StringVal("vpc-123"),
			"cidr_block": cty.StringVal("10.0.0.0/16"),
		})},
	}
	ctx := NewEvalContext(nil, resources, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, "aws_vpc.this.id"))
	require.NoError(t, err)
	require.Equal(t, "vpc-123", val.AsString())
}

func TestEvaluateModuleOutputRef(t *testing.T) {
	moduleOutputs := map[string]map[string]cty.Value{
		"vpc": {"vpc_id": cty.StringVal("vpc-123")},
	}
	ctx := NewEvalContext(nil, nil, moduleOutputs)
	val, err := ctx.EvaluateExpression(parseExpr(t, "module.vpc.vpc_id"))
	require.NoError(t, err)
	require.Equal(t, "vpc-123", val.AsString())
}

func TestEvaluateFunction(t *testing.T) {
	ctx := NewEvalContext(nil, nil, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, `join("-", ["a", "b", "c"])`))
	require.NoError(t, err)
	require.Equal(t, "a-b-c", val.AsString())
}

func TestEvaluateConditional(t *testing.T) {
	vars := map[string]cty.Value{"enable": cty.True}
	ctx := NewEvalContext(vars, nil, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, `var.enable ? "yes" : "no"`))
	require.NoError(t, err)
	require.Equal(t, "yes", val.AsString())
}

func TestEvaluateForExpression(t *testing.T) {
	vars := map[string]cty.Value{
		"names": cty.ListVal([]cty.Value{cty.StringVal("a"), cty.StringVal("b")}),
	}
	ctx := NewEvalContext(vars, nil, nil)
	val, err := ctx.EvaluateExpression(parseExpr(t, `[for s in var.names : upper(s)]`))
	require.NoError(t, err)
	require.Equal(t, 2, val.LengthInt())
}

func TestEvaluateUnsupportedFunction_Fallback(t *testing.T) {
	ctx := NewEvalContext(nil, nil, nil)
	_, err := ctx.EvaluateExpression(parseExpr(t, `totally_fake_function("x")`))
	require.Error(t, err)
}
```

- [ ] **Step 3: Implement**

```go
// pkg/hcl/evaluator.go
package hcl

type EvalContext struct {
	hclCtx *hcl.EvalContext
}

// NewEvalContext creates an HCL evaluation context populated with TF state data.
func NewEvalContext(
	variables map[string]cty.Value,
	resources map[string]map[string]cty.Value, // resourceType -> name -> attributes object
	moduleOutputs map[string]map[string]cty.Value, // moduleName -> outputName -> value
) *EvalContext {
	ctx := &hcl.EvalContext{
		Variables: map[string]cty.Value{},
		Functions: buildFunctionTable(),
	}

	// Populate var.* namespace
	if len(variables) > 0 {
		ctx.Variables["var"] = cty.ObjectVal(variables)
	}

	// Populate resource type namespaces
	for resType, instances := range resources {
		ctx.Variables[resType] = cty.ObjectVal(instances)
	}

	// Populate module.* namespace
	if len(moduleOutputs) > 0 {
		modVals := map[string]cty.Value{}
		for modName, outputs := range moduleOutputs {
			modVals[modName] = cty.ObjectVal(outputs)
		}
		ctx.Variables["module"] = cty.ObjectVal(modVals)
	}

	return &EvalContext{hclCtx: ctx}
}

// buildFunctionTable returns the Terraform-compatible function table.
// Combines standard functions from zclconf/go-cty stdlib with
// Terraform-specific functions from pulumi/opentofu.
func buildFunctionTable() map[string]function.Function {
	funcs := map[string]function.Function{
		// From zclconf/go-cty/cty/function/stdlib (standard functions)
		"join": stdlib.JoinFunc, "split": stdlib.SplitFunc,
		"upper": stdlib.UpperFunc, "lower": stdlib.LowerFunc,
		"length": stdlib.LengthFunc, "flatten": stdlib.FlattenFunc,
		"merge": stdlib.MergeFunc, "keys": stdlib.KeysFunc,
		"values": stdlib.ValuesFunc, "contains": stdlib.ContainsFunc,
		"regex": stdlib.RegexFunc, "regexall": stdlib.RegexAllFunc,
		"jsonencode": stdlib.JSONEncodeFunc, "jsondecode": stdlib.JSONDecodeFunc,
		// ... all stdlib functions

		// From github.com/pulumi/opentofu/lang/funcs (Terraform-specific)
		"cidrhost": funcs.CidrHostFunc, "cidrnetmask": funcs.CidrNetmaskFunc,
		"cidrsubnet": funcs.CidrSubnetFunc, "cidrsubnets": funcs.CidrSubnetsFunc,
		"timestamp": funcs.TimestampFunc, "timeadd": funcs.TimeAddFunc,
		"base64encode": funcs.Base64EncodeFunc, "base64decode": funcs.Base64DecodeFunc,
		"md5": funcs.Md5Func, "sha256": funcs.Sha256Func,
		"parseint": funcs.ParseIntFunc, "replace": funcs.ReplaceFunc,
		// ... all opentofu/lang/funcs functions
	}
	return funcs
}

func (e *EvalContext) EvaluateExpression(expr hcl.Expression) (cty.Value, error) {
	val, diags := expr.Value(e.hclCtx)
	if diags.HasErrors() {
		return cty.NilVal, fmt.Errorf("expression evaluation failed: %s", diags.Error())
	}
	return val, nil
}
```

- [ ] **Step 4: Run tests, commit**

### PR 8 Submission

```bash
git push -u origin feat/mc-08-evaluator
gh pr create --base feat/mc-07-callsite-tfvars \
  --title "feat(modules): HCL expression evaluator with TF state context" \
  --body "..." # similar format
```

---

## PR 9: Component State Population + Auto-Discovery + Gap Fixes

**Scope change:** PR 9 now absorbs auto-discovery (originally PR 10) and fixes all gaps identified during implementation review. The cty-to-Pulumi conversion and basic HCL population pipeline are already implemented — this PR completes the remaining work.

### Already implemented (needs restacking onto mc-09)

- `pkg/hcl/convert.go` — `CtyValueToPulumiPropertyValue`, `CtyMapToPulumiPropertyMap`, `PulumiPropertyMapToCtyMap`
- `pkg/hcl/convert_test.go` — type conversion tests
- `pkg/component_populate.go` — `populateComponentsFromHCL` with basic input evaluation and schema validation wiring
- `pkg/hcl/discovery.go` — `DiscoverModuleSources`, `IsLocalModuleSource`
- `pkg/hcl/discovery_test.go` — local/remote source discovery tests
- `cmd/stack.go` — `--module-source-map` and `--module-schema` flags
- Schema validation integration tests with type token overrides

### File Map (remaining work)

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `cmd/stack.go` | Add `--component-inputs` flag |
| Modify | `pkg/component_populate.go` | Fix gaps: variable defaults, resource attr refs, output values from raw state; gate input population on flag |
| Create | `pkg/component_metadata.go` | Component schema metadata struct + JSON serialization |
| Create | `pkg/component_metadata_test.go` | Tests for metadata generation |
| Modify | `pkg/state_adapter.go` | Thread raw state file path, TF resource attributes, and `populateComponentInputs` flag into pipeline; write metadata file |
| Modify | `pkg/state_adapter_test.go` | Integration tests for HCL population + schema validation + metadata file |

### Task 11.5: Add `--component-inputs` flag and schema metadata file

**New flag:** `--component-inputs` (default: `true`)
- `true` → populate component inputs in state (for component providers / IDP)
- `false` → empty inputs `{}` + write `component-schemas.json` sidecar file (for single-language components)

- [ ] **Step 1: Write failing tests**

```go
func TestComponentMetadata_GeneratesSchemaFile(t *testing.T) {
	// --component-inputs=false + HCL source available
	// → component inputs = {} in state
	// → component-schemas.json written with variable/output declarations
}

func TestComponentMetadata_NotGeneratedWhenInputsEnabled(t *testing.T) {
	// --component-inputs=true (default)
	// → component inputs populated in state
	// → no component-schemas.json file written
}
```

- [ ] **Step 2: Create `pkg/component_metadata.go`**

```go
type ComponentSchemaMetadata struct {
	Components map[string]ComponentSchema `json:"components"`
}

type ComponentSchema struct {
	Type    string                `json:"type"`
	Source  string                `json:"source,omitempty"`
	Inputs  []ComponentFieldMeta  `json:"inputs"`
	Outputs []ComponentFieldMeta  `json:"outputs"`
}

type ComponentFieldMeta struct {
	Name     string      `json:"name"`
	Type     string      `json:"type"`
	Required bool        `json:"required,omitempty"`
	Default  interface{} `json:"default,omitempty"`
}
```

- [ ] **Step 3: Add flag to `cmd/stack.go`**

```go
var componentInputs bool
cmd.Flags().BoolVar(&componentInputs, "component-inputs", true,
	"Populate component inputs in state (true for component providers, false for single-language components)")
```

- [ ] **Step 4: Gate input population in `populateComponentsFromHCL`**

When `populateComponentInputs=false`:
- Skip call-site expression evaluation for inputs
- Still parse HCL for interface extraction (variables + outputs)
- Collect `ComponentSchemaMetadata` from parsed variables/outputs
- Return metadata to caller for writing to sidecar file

When `populateComponentInputs=true`:
- Existing behavior: evaluate and populate inputs in state

- [ ] **Step 5: Write metadata file in `state_adapter.go`**

After `populateComponentsFromHCL` returns, if metadata is non-nil, write `component-schemas.json` to the same directory as the output state file.

- [ ] **Step 6: Run tests, commit**

---

### Task 12: Merge variable defaults into component inputs (when `--component-inputs=true`)

**Location:** `pkg/component_populate.go:85-117`

**Problem:** When a module call site omits an argument that has a default value in the `variable` block, the component's inputs should include the default value. Currently, only explicitly passed arguments are evaluated.

**Example:** If `variable "enable_dns" { default = true }` and the call site doesn't pass `enable_dns`, the component input should still have `enable_dns: true`.

- [ ] **Step 1: Write failing test**

```go
func TestPopulateComponentInputs_VariableDefaults(t *testing.T) {
	// Module has variable "prefix" (required) and variable "suffix" { default = "-prod" }
	// Call site passes prefix = "test" but not suffix
	// Component inputs should have both: prefix="test", suffix="-prod"
}
```

- [ ] **Step 2: Implement**

After evaluating call-site arguments, parse module variables via `ParseModuleVariables`. For any variable with a default that is NOT in the call-site arguments, add the default value to inputs.

```go
// In populateComponentsFromHCL, after call-site argument evaluation:
if sourcePath != "" {
	vars, err := hclpkg.ParseModuleVariables(sourcePath)
	if err == nil {
		for _, v := range vars {
			if _, alreadySet := inputs[resource.PropertyKey(v.Name)]; !alreadySet && v.Default != nil {
				inputs[resource.PropertyKey(v.Name)] = hclpkg.CtyValueToPulumiPropertyValue(*v.Default)
			}
		}
	}
}
```

- [ ] **Step 3: Run tests, commit**

### Task 13: Add resource attribute refs to eval context

**Location:** `pkg/component_populate.go:100` — `NewEvalContext` called with `nil` for resources.

**Problem:** HCL expressions that reference resource attributes (e.g., `aws_vpc.this.id`, `module.vpc.vpc_id`) fail to evaluate because the eval context has no resource data.

- [ ] **Step 1: Write failing test**

```go
func TestPopulateComponentInputs_ResourceAttrRef(t *testing.T) {
	// Call site has: subnet_id = aws_vpc.main.id
	// TF state has aws_vpc.main with id = "vpc-123"
	// Component input subnet_id should be "vpc-123"
}
```

- [ ] **Step 2: Implement**

Build resource attribute map from TF state JSON and pass to `NewEvalContext`. The state JSON has all resource attributes available via `tfjson.StateResource.AttributeValues`.

```go
// In state_adapter.go or component_populate.go:
func buildResourceAttrMap(tfState *tfjson.State) map[string]map[string]cty.Value {
	resources := map[string]map[string]cty.Value{}
	tofu.VisitResources(tfState, func(r *tfjson.StateResource) error {
		// Parse resource type and name from address
		// Convert r.AttributeValues (map[string]interface{}) to cty.Value
		// Store as resources[resourceType][resourceName] = cty.ObjectVal(attrs)
		return nil
	}, &tofu.VisitOptions{})
	return resources
}

// Pass to NewEvalContext:
evalCtx := hclpkg.NewEvalContext(evalVars, resourceAttrs, moduleOutputs)
```

- [ ] **Step 3: Run tests, commit**

### Task 14: Read output values from raw TF state

**Location:** `pkg/component_populate.go:119-134` — outputs are placeholders.

**Problem:** Component outputs use empty string placeholders instead of actual values from `opentofu/states.Module.OutputValues`.

**Design spec says:**
> Output values can be sourced from raw state — Since `opentofu/states.Module.OutputValues` contains resolved output values, we can read them directly from the raw `.tfstate` file.

- [ ] **Step 1: Write failing test**

```go
func TestPopulateComponentOutputs_FromRawState(t *testing.T) {
	// Raw .tfstate has module.vpc.OutputValues = {"vpc_id": "vpc-123"}
	// Component outputs should have vpc_id = "vpc-123" (not empty string)
}
```

- [ ] **Step 2: Implement**

Thread raw state file path into `populateComponentsFromHCL`. The tool already has `pkg/statefile/` for reading raw state.

```go
// Read raw state to get module output values
rawState, err := statefile.ReadRawState(rawStatePath)
if err == nil {
	for modulePath, mod := range rawState.Modules {
		// mod.OutputValues is map[string]*OutputValue
		// Each OutputValue has a .Value field (cty.Value)
		outputMap := resource.PropertyMap{}
		for name, ov := range mod.OutputValues {
			outputMap[resource.PropertyKey(name)] = hclpkg.CtyValueToPulumiPropertyValue(ov.Value)
		}
		// Match modulePath to component and set outputs
	}
}
```

Fallback: if raw state unavailable, keep current behavior (output names from HCL declarations with empty values).

- [ ] **Step 3: Run tests, commit**

### Task 15: Integration tests for HCL population + schema validation

- [ ] **Step 1: Write integration tests**

```go
func TestConvertWithHCLPopulation(t *testing.T) {
	// Real TF state + HCL source → verify component inputs/outputs populated
	// Inputs include both call-site args and variable defaults
}

func TestConvertWithHCLPopulation_FallbackNoSource(t *testing.T) {
	// No HCL source available → component has empty inputs/outputs, warning logged
}

func TestConvertWithSchemaValidation(t *testing.T) {
	// Real TF state + HCL source + schema → schema validation passes
}

func TestConvertWithSchemaValidation_Mismatch(t *testing.T) {
	// Real TF state + HCL source + schema with extra required field → validation error
}

func TestConvertWithResourceAttrRefs(t *testing.T) {
	// Call site references resource attributes → inputs populated correctly
}

func TestConvertWithOutputsFromRawState(t *testing.T) {
	// Raw state available → outputs populated with real values
}

func TestConvertWithComponentInputsFalse(t *testing.T) {
	// --component-inputs=false → component inputs = {} in state
	// → component-schemas.json sidecar file written with interface declarations
}

func TestConvertWithComponentInputsTrue(t *testing.T) {
	// --component-inputs=true (default) → component inputs populated in state
	// → no component-schemas.json file written
}
```

- [ ] **Step 2: Run full test suite, commit**

### Task 16: Update terraform-migrate skill with flag guidance

Update the terraform-migrate agent skill (if it exists) or document guidance for agents using the tool. The skill should instruct agents on when to use each flag:

- [ ] **Step 1: Find and update the skill**

Add guidance covering all module-related flags:

| Flag | When to use |
|------|-------------|
| `--component-inputs` (default: true) | `true` for component providers / IDP registration (common case). `false` for single-language inline ComponentResource classes. |
| `--module-type-map` | Override auto-derived type tokens when target Pulumi code uses custom component types. |
| `--module-source-map` | Map modules to HCL source paths when auto-discovery can't find them (remote modules, non-standard layouts). |
| `--module-schema` | Provide Pulumi package schema for validation when wrapping with existing component providers. |
| `--no-module-components` | Disable component generation entirely (flat mode). Use only for backward compatibility. |

Key decision tree for agents:
1. Will the generated code use a component provider (terraform-module plugin, IDP-registered)? → `--component-inputs=true` (default)
2. Will the generated code use inline ComponentResource classes? → `--component-inputs=false`, consume `component-schemas.json` for code generation
3. Should modules be represented as components at all? → Default yes. Use `--no-module-components` only if the user explicitly wants flat migration.

- [ ] **Step 2: Commit**

### PR 9 Submission

```bash
git push -u origin feat/mc-09-state-population
gh pr create --base feat/mc-08-evaluator \
  --title "feat(modules): complete component state population with gap fixes" \
  --body "$(cat <<'EOF'
## Summary
- `--component-inputs` flag: true populates inputs in state (component providers), false writes sidecar schema metadata (single-language components)
- Merge variable defaults into component inputs when enabled
- Build resource attribute map from TF state for eval context (enables `aws_vpc.this.id` refs)
- Read output values from raw `.tfstate` via `opentofu/states.Module.OutputValues`
- Auto-discover local module sources from root HCL files
- Schema metadata sidecar file (`component-schemas.json`) for code generator
- Integration tests for all modes

## Test plan
- [ ] --component-inputs=false produces empty inputs + sidecar file
- [ ] --component-inputs=true (default) populates inputs, no sidecar
- [ ] Variable defaults merged into inputs when enabled
- [ ] Resource attribute refs evaluate correctly
- [ ] Output values read from raw state (not placeholders)
- [ ] Auto-discovery finds local sources, skips remote
- [ ] Schema validation integration tests pass
- [ ] Full test suite passes

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## PR 10: Comprehensive E2E State Translation Tests

**Scope change:** PR 10 is now comprehensive E2E tests for the full state translation pipeline. Tests validate translated Pulumi state values — not program generation or `pulumi preview`. The tool's job is to translate as much state as possible, and fail or warn on elements it can't translate.

### Approach

- **Real fixtures from real deployments**: Deploy with `tofu apply` during development (using `team-ce/aws/pulumi-ce` ESC env, or `pulumi/default/dev-sandbox` for Route53 domains). Capture state with `tofu show -json`. Commit fixtures. Tests don't deploy infrastructure.
- **AWS provider**: Real-world modules from `terraform-aws-modules/*`. Also keep random-provider fixtures for simpler structure tests.
- **Test file**: `pkg/state_adapter_test.go`
- **HCL fixtures**: `pkg/testdata/tf_<name>/` with `.tf` files matching state
- **Schema fixtures**: `pkg/testdata/schemas/`

### File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Copy | `pkg/testdata/tofu_state_dns_to_db.json` | DNS-to-DB state from existing deployment |
| Copy | `pkg/testdata/tf_dns_to_db/` | HCL source matching DNS-to-DB state |
| Create | `pkg/testdata/tf_multi_resource_module/` + state | Multi-resource module (random provider) |
| Create | `pkg/testdata/tf_deep_nested_mixed/` + state | 3-level nesting with mixed count/for_each |
| Create | `pkg/testdata/tf_complex_expressions/` + state | Function calls + conditionals in args |
| Create | `pkg/testdata/tf_tfvars_resolution/` + state | Tfvars + variable defaults |
| Create | `pkg/testdata/tf_special_key_modules/` + state | Special chars in for_each keys |
| Create | `pkg/testdata/schemas/zoo_component_schema.json` | Schema fixtures |
| Modify | `pkg/state_adapter_test.go` | All new test functions |

### Fixtures

#### Fixture 1: DNS-to-DB Stack (already deployed, state captured)

**Source**: `tf_stack_dns_to_db` repo
**Module tree** (~90 managed resources, 2 data sources):
```
root (7 managed: aws_eip, aws_route53_record, null_resource, 6x aws_lb_target_group_attachment)
├── module.vpc (25 managed) — terraform-aws-modules/vpc/aws v5.4.0
├── module.public_bastion_sg (3 managed) — security-group
├── module.private_sg (5 managed) — security-group
├── module.loadbalancer_sg (5 managed) — security-group
├── module.rdsdb_sg (3 managed) — security-group
├── module.ec2_public (4 managed) — ec2-instance (single)
├── module.ec2_private_app1["0"] (4 managed) — ec2-instance (for_each)
├── module.ec2_private_app1["1"] (4 managed)
├── module.ec2_private_app2["0"] (4 managed) — ec2-instance (for_each)
├── module.ec2_private_app2["1"] (4 managed)
├── module.ec2_private_app3["0"] (3 managed) — ec2-instance (for_each)
├── module.ec2_private_app3["1"] (3 managed)
├── module.alb (10 managed) — alb v9.4.0
├── module.acm (3 managed) — acm v5.0.0
└── module.rdsdb (nested submodules)
    ├── module.rdsdb.module.db_instance (0 managed, 2 data)
    ├── module.rdsdb.module.db_option_group (1 managed)
    ├── module.rdsdb.module.db_parameter_group (1 managed)
    └── module.rdsdb.module.db_subnet_group (1 managed)
```

**Key HCL features**: for_each, function chaining (`element`, `tonumber`), `templatefile`, string interpolation, module-to-module refs, nested modules, multiple providers, sensitive values.

#### Fixture 2: Multi-resource module (random provider)

```
module.zoo → random_pet.animal + random_string.tag + random_integer.count
```

#### Fixture 3: Deep nested mixed (random provider)

```
module.env["dev"].module.svc[0].module.instance.random_pet.this
module.env["prod"].module.svc[1].module.instance.random_pet.this
```

#### Fixture 4: Complex HCL expressions (random provider)

```hcl
module "svc" {
  count      = 2
  prefix     = join("-", ["svc", format("%02d", count.index)])
  is_primary = count.index == 0 ? true : false
  label      = upper("service-${count.index}")
}
```

#### Fixture 5: Tfvars + variable defaults (random provider)

```hcl
variable "env" { type = string }            # from tfvars: "staging"
variable "team" { default = "platform" }    # from default
module "named" { prefix = var.env ; suffix = var.team }
```

#### Fixture 6: Special key sanitization (random provider)

```
module.region["us-east-1"]        → region-us-east-1
module.region["eu-west-1/zone-a"] → region-eu-west-1-zone-a
module.region["ap.southeast.2"]   → region-ap-southeast-2
```

### Test Functions

#### DNS-to-DB (Fixture 1) — real-world complexity

| Test | Config | Key Assertions |
|------|--------|----------------|
| `TestConvertDnsToDb` | components=true, no HCL | ~15 components. Resources parented correctly. Nested rdsdb submodules produce `$` URN chain. for_each instances share type token. Root resources parented to Stack. |
| `TestConvertDnsToDb_TypeOverrides` | type overrides for vpc + ec2 | Custom types applied to all for_each instances. Others keep derived types. |
| `TestConvertDnsToDb_FlatMode` | components=false | 0 components. ~90 resources all parented to Stack. |
| `TestConvertDnsToDb_WithHCL` | + tfSourceDir | Root-level module inputs populated. Nested rdsdb inputs NOT populated (known limitation). |

#### Multi-resource module (Fixture 2)

| Test | Config | Key Assertions |
|------|--------|----------------|
| `TestConvertMultiResourceModule` | components=true | 1 component `zoo`, 3 children parented to it. |
| `TestConvertMultiResourceModule_WithHCL` | + tfSourceDir | Inputs populated. Outputs have correct keys. |
| `TestConvertMultiResourceModule_SchemaPass` | + matching schema | No error. |
| `TestConvertMultiResourceModule_SchemaExtraOutput` | + mismatch schema | Error: "not in schema". |

#### Deep nested mixed (Fixture 3)

| Test | Config | Key Assertions |
|------|--------|----------------|
| `TestConvertDeepNestedMixed` | components=true | 10 components (2 env + 4 svc + 4 instance). URN type chain: `Env$Svc$Instance`. |
| `TestConvertDeepNestedMixed_FlatMode` | components=false | 0 components, all parented to Stack. |

#### Complex HCL expressions (Fixture 4)

| Test | Config | Key Assertions |
|------|--------|----------------|
| `TestConvertComplexExpressions` | + tfSourceDir | svc-0: prefix="svc-00", is_primary=true, label="SERVICE-0". |

#### Tfvars + defaults (Fixture 5)

| Test | Config | Key Assertions |
|------|--------|----------------|
| `TestConvertTfvarsResolution` | + tfSourceDir (with tfvars) | prefix="staging", suffix="platform" (from default). |

#### Special keys (Fixture 6)

| Test | Config | Key Assertions |
|------|--------|----------------|
| `TestConvertSpecialKeyModules` | components=true | 3 components. Names sanitized correctly. No collision. |

#### Error cases

| Test | Config | Key Assertions |
|------|--------|----------------|
| `TestConvertSchemaFileNotFound` | nonexistent schema path | Error: "reading schema file" |
| `TestConvertSchemaTypeNotFound` | wrong type token | Error: "not found in schema" |
| `TestConvertFlatMode_AllFixtures` | Table-driven, components=false | 0 components for all fixtures. |

### Implementation Order

1. **Copy DNS-to-DB fixture** — state JSON + HCL from existing deployment
2. **Create + deploy random-provider fixtures** — write HCL, `tofu apply`, capture state
3. **Create schema fixtures** by hand
4. **Write tests** — DNS-to-DB first, then simpler fixtures
5. **Run and iterate**

### Verification

```bash
go test ./pkg/ -run "TestConvertDnsToDb|TestConvertMultiResource|TestConvertDeepNested|TestConvertComplex|TestConvertTfvars|TestConvertSpecialKey|TestConvertSchema|TestConvertFlatMode_All" -v -count=1
go test ./pkg/... -count=1
```

### Known Limitations (tests document, don't fix)

1. **Nested module HCL population**: `populateComponentsFromHCL` only parses call sites from root dir.
2. **Root-level resources**: Resources not inside a module are parented to Stack, not to any component.

### PR 10 Submission

```bash
git push -u origin feat/mc-10-e2e-tests
gh pr create --base feat/mc-09-state-population \
  --title "test(modules): comprehensive E2E state translation tests" \
  --body "$(cat <<'EOF'
## Summary
- Real-world E2E tests using DNS-to-DB stack (~90 resources, nested modules, for_each)
- Random-provider fixtures for targeted feature coverage
- Schema validation integration tests
- Flat mode sweep across all fixtures

## Test plan
- [ ] DNS-to-DB: components, type overrides, flat mode, HCL population
- [ ] Multi-resource module: component wrapping, schema validation
- [ ] Deep nested: 3-level hierarchy with mixed count/for_each
- [ ] Complex expressions: function calls, conditionals
- [ ] Tfvars + variable defaults
- [ ] Special key sanitization
- [ ] Error cases: schema not found, type mismatch
- [ ] Full test suite passes

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```
