# TFC-Compatible Remote State Pull for `module-map`

## Problem

The `module-map` command requires a local `--state-file`. For workspaces using remote backends (Scalr, TFC, TFE), users must manually export state before running the tool. We want `module-map` to pull state directly from TFC-compatible APIs without running `tofu init`.

## Design

### New package: `pkg/tfc/client.go`

A minimal TFC-compatible HTTP client.

```go
type Client struct {
    Hostname string         // e.g. "veridos-america.scalr.io" (no scheme)
    Token    string
    HTTP     *http.Client
}

func (c *Client) StatePull(ctx context.Context, org, workspace string) ([]byte, error)
```

**URL construction:** The client builds URLs as `https://{hostname}/...`. `Hostname` is stored without a scheme. However, if `Hostname` already contains a scheme (e.g. `http://127.0.0.1:PORT` from tests), use it as-is. Check with `strings.HasPrefix(hostname, "http://") || strings.HasPrefix(hostname, "https://")`.

**StatePull flow:**

1. `GET {baseURL}/.well-known/terraform.json` — discover API base path. Try `"tfe.v2"` key first (standard for TFC/TFE/Scalr), fall back to `"state.v2"` if absent. Error if neither key exists or the response is not valid JSON: `"service discovery failed: {hostname} did not return a valid /.well-known/terraform.json"`.
2. `GET {base}/organizations/{org}/workspaces/{workspace}` — extract `data.id` (workspace ID)
3. `GET {base}/workspaces/{id}/current-state-version` — extract `data.attributes.hosted-state-download-url`
4. `GET {download_url}` with Bearer token — return raw state bytes

All requests use `Authorization: Bearer {token}` and `Content-Type: application/vnd.api+json`. No retries.

Error handling:
- 401 → `"authentication failed for {hostname}"` (the caller in `cmd/module_map.go` wraps this with the env var name: `"authentication failed for {hostname}: check token in env var {tokenEnv}"`)
- 404 on workspace lookup → `"workspace {org}/{workspace} not found on {hostname}"`
- 404 on state version → `"no state found for workspace {org}/{workspace}"`
- Non-JSON or malformed response bodies → `"unexpected response from {url}: {status code}"`
- Network/other → wrap with context

### Changes to `cmd/module_map.go`

New optional flags:
- `--hostname` — TFC-compatible API host
- `--organization` — organization/account name
- `--workspace` — workspace name
- `--token-env` — name of env var containing the API token

`--state-file` becomes optional. Remove the existing `cmd.MarkFlagRequired("state-file")` call and replace with custom validation in `RunE`:
- Either `--state-file` OR all four remote flags must be provided
- If `--token-env` is set, the named env var must be non-empty at runtime
- If any remote flag is set, all four must be set — error message lists which flags are missing: `"--hostname, --organization, --workspace, and --token-env are all required when using remote state (missing: --workspace, --token-env)"`
- `--state-file` and remote flags are mutually exclusive — if both provided: `"--state-file and remote flags (--hostname, --organization, --workspace, --token-env) are mutually exclusive"`

### Changes to `pkg/generate_module_map.go`

Add a `RemoteStateOptions` struct:

```go
type RemoteStateOptions struct {
    Hostname     string
    Organization string
    Workspace    string
    Token        string
}
```

`GenerateModuleMap` new signature:

```go
func GenerateModuleMap(ctx context.Context, tfDir, stateFilePath, outputPath, stackName, projectName string, remote *RemoteStateOptions) error
```

Mutual exclusivity is enforced at the CLI layer (`cmd/module_map.go`). `GenerateModuleMap` trusts that exactly one of `stateFilePath` or `remote` is provided. If both are non-empty/non-nil (programming error), error: `"stateFilePath and remote are mutually exclusive"`.

When `stateFilePath` is empty and `remote` is non-nil:

1. Create `tfc.Client{Hostname: opts.Hostname, Token: opts.Token}`
2. Call `client.StatePull(ctx, opts.Organization, opts.Workspace)` → `[]byte`
3. Pass bytes directly into the state detection/loading functions (no temp file)

### Refactor `DetectStateFormat` and `LoadRawState` to accept `[]byte`

Currently both read from a file path. Refactor to:

- `DetectStateFormat(data []byte) StateFormat` — check for `"format_version"` key in the JSON
- `LoadRawState(data []byte) (*states.State, error)` — wrap in `bytes.Reader`, pass to `statefile.Read()`

The file-based entry point reads the file into `[]byte` first, then calls these functions. This eliminates the need for temp files in the remote path.

For the `StateFormatTofuShowJSON` case: unmarshal `tfjson.State` directly from the byte slice with `json.Unmarshal`, bypassing `LoadTerraformState` entirely (which runs `tofu init`). Then feed into existing `rawStateFromTfjson`.

**Note:** TFC state downloads are always raw tfstate format (`StateFormatRaw`), not `tofu show -json` format. The `StateFormatTofuShowJSON` bytes path is only needed for the local `--state-file` refactor to avoid reading the file twice.

### What doesn't change

- `pkg/tofu/loader.go` — untouched (remote path bypasses it entirely)
- `BuildModuleMap`, `rawStateFromTfjson`, provider resolution — unchanged
- All existing `--state-file` behavior preserved (reads file to bytes, then same path)

## CLI Examples

```bash
# Existing: local state file (unchanged)
pulumi-terraform-migrate module-map \
  --from ./environments/develop \
  --state-file ./terraform.tfstate \
  --out ./module-map.json \
  --pulumi-stack dev \
  --pulumi-project myproject

# New: pull from TFC-compatible remote
pulumi-terraform-migrate module-map \
  --from ./environments/develop \
  --hostname veridos-america.scalr.io \
  --organization valid \
  --workspace dmvhm-infrastructure-develop \
  --token-env SCALR_TOKEN \
  --out ./module-map.json \
  --pulumi-stack dev \
  --pulumi-project myproject
```

## Testing

- Unit test `pkg/tfc`: use `httptest.NewServer` to mock the well-known endpoint, workspace lookup, state version, and state download. Pass the server's URL as `Hostname` to `tfc.Client` (the client builds URLs from `Hostname`, so a `httptest` URL works directly).
- Unit test `DetectStateFormat` and `LoadRawState` with byte slices (no file I/O)
- Integration test: use `httptest.NewServer` and pass its host to `module-map` via `--hostname`. Requires `--from` to point to a directory with valid `.tf` files for config loading.
