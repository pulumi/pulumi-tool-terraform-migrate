# Decouple State Translation from Pulumi Workspace

**Date**: 2026-04-01
**Status**: Design approved

## Problem

`TranslateState` requires a fully initialized Pulumi workspace (created via `pulumi new` + `pulumi up`) just to extract three values: stack name, project name, and a Stack resource URN. This couples the translation pipeline to filesystem state and external processes unnecessarily.

Consequences:
1. **Slow tests** — `createPulumiStack` runs `pulumi new` (clones templates repo over network) + `pulumi up` per test (~18s each). Tests can't run in parallel because concurrent `pulumi new` calls cause EOF errors on the git clone.
2. **Unnecessary coupling** — The translation pipeline is logically a pure function of (TF state + config strings) → Pulumi state. Requiring a real workspace is accidental complexity.
3. **Fragile CI** — Network-dependent template cloning in tests introduces flakiness.

## Design

### Remove `GetDeployment` dependency from `TranslateState`

`GetDeployment` currently provides three things to `InsertResourcesIntoDeployment`:
- `stackName` (from `pulumi stack ls --json`)
- `projectName` (from `workspace.ProjectSettings()`)
- `deployment` (from `workspace.ExportStack()` — contains a single Stack resource)

`InsertResourcesIntoDeployment` uses the deployment **only** to find the Stack resource URN via `findStackResource()`. That URN is deterministically constructed from `stackName` and `projectName`: `urn:pulumi:<stack>::<project>::pulumi:pulumi:Stack::<project>-<stack>`.

### Pure functions vs. workspace fallback

`TranslateState` and `InsertResourcesIntoDeployment` become pure functions of strings — no workspace, no processes, no filesystem. `TranslateAndWriteState` retains the workspace fallback path for auto-detecting stack/project names when CLI flags are not provided.

### New signatures

**`InsertResourcesIntoDeployment`** — drops the `deployment` parameter, constructs the Stack resource internally:

```go
func InsertResourcesIntoDeployment(
    state *PulumiState, stackName, projectName string,
) (apitype.DeploymentV3, error)
```

Validates `stackName` and `projectName` are non-empty. Constructs the Stack `ResourceV3` entry and inserts it as the first resource, then appends providers, components, and custom resources as before.

**`TranslateState`** — drops `pulumiProgramDir`, takes `stackName` and `projectName` directly:

```go
func TranslateState(
    ctx context.Context, tfState *tfjson.State,
    providerVersions map[string]string,
    stackName, projectName string,
    enableComponents bool, typeOverrides map[string]string,
) (*TranslateStateResult, error)
```

No longer calls `GetDeployment`. Passes `stackName` and `projectName` to `InsertResourcesIntoDeployment`.

**`TranslateAndWriteState`** — resolves names from flags or workspace fallback:

```go
func TranslateAndWriteState(
    ctx context.Context, tfDir, pulumiProgramDir, outputFile string,
    requiredProvidersOutputFilePath string, strict bool,
    enableComponents bool, typeOverrides map[string]string,
    stackNameOverride, projectNameOverride string,
) error
```

Resolution logic:
1. If `stackNameOverride` is non-empty, use it. Otherwise call `getStackName(pulumiProgramDir)`.
2. If `projectNameOverride` is non-empty, use it. Otherwise call `getProjectName(pulumiProgramDir)`.

The `--to` flag (`pulumiProgramDir`) remains always required. Even when both `--pulumi-stack` and `--pulumi-project` are provided, the Pulumi program directory is still needed by the broader tool for other operations (code generation, `pulumi stack import`, etc.). The override flags skip only the workspace queries for name resolution, not the workspace itself.

**`getProjectName`** — new function, reads `Pulumi.yaml` directly:

```go
func getProjectName(projectDir string) (string, error)
```

Uses `os.ReadFile` + `yaml.Unmarshal` to extract the `name` field from `Pulumi.yaml`. No Automation API, no subprocess.

**`getStackName`** — unchanged, still calls `pulumi stack ls --json`. The asymmetry with `getProjectName` is acceptable: stack name requires knowing the *selected* stack which is Pulumi CLI state (`.pulumi/` directory structure varies by backend), while project name is a simple YAML field. Both are bypassed when override flags are provided.

### Deleted code

- `GetDeployment` — no longer needed
- `findStackResource` — no longer needed (Stack resource is constructed, not found)
- `DeploymentResult` struct — no longer needed

### CLI flags

Add optional flags to `cmd/stack.go`:
- `--pulumi-stack` — override stack name (skip auto-detection)
- `--pulumi-project` — override project name (skip auto-detection)

When omitted, fall back to reading from the Pulumi workspace at `pulumiProgramDir`.

### Test changes

**`pkg/state_adapter_test.go`:**
- Delete `createPulumiStack` — no longer needed
- `translateStateFromJson` passes string literals (`"dev"`, `"test-project"`) instead of a workspace path
- All tests become parallel-safe with zero filesystem or process dependencies
- Add `t.Parallel()` to all test functions

**`pkg/pulumi_state_test.go`:**
- Update `InsertResourcesIntoDeployment` tests to new signature (drop deployment parameter, pass stack/project strings)
- Delete `TestInsertResourcesIntoDeployment_ZeroResources` and `TestInsertResourcesIntoDeployment_MultipleResources` — they test "deployment must have exactly 1 Stack resource" validation which no longer exists
- Add `TestInsertResourcesIntoDeployment_EmptyStackName` and `TestInsertResourcesIntoDeployment_EmptyProjectName` — test the replacement validation (non-empty strings required)
- Delete `TestGetDeployment` — function no longer exists
- Delete `runCommand` helper — only used by `TestGetDeployment`

**`test/translate_test.go`:**
- Integration tests keep `createPulumiStack` (they need a real workspace for `pulumi stack import` + `pulumi preview`)
- Update `TranslateAndWriteState` calls to pass override strings

### Existing behavior preserved

- When no flags provided, tool auto-detects from workspace (same as today)
- Stack resource in output has identical URN format
- Insertion order unchanged: Stack → providers → components → custom resources
- The validation "stack must have exactly 1 resource" is replaced by "stackName and projectName must be non-empty" — functionally equivalent since the tool creates the Stack resource itself

## Backward Compatibility

- CLI: no breaking changes (new flags are optional, `--to` remains required)
- Output format: identical Pulumi state JSON
- `getStackName` unchanged (still calls `pulumi stack ls --json`)
