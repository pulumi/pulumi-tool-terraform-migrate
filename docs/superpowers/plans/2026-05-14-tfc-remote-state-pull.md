# TFC-Compatible Remote State Pull Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow `module-map` to pull state directly from TFC-compatible APIs (Scalr, TFC, TFE) without requiring a local state file or running `tofu init`.

**Architecture:** New `pkg/tfc` package provides a minimal HTTP client for the TFC state API. `DetectStateFormat` and `LoadRawState` are refactored from file-path-based to bytes-based. The `module-map` command gets new flags for remote config and wires them through `GenerateModuleMap` to the TFC client.

**Tech Stack:** Go, `net/http`, `net/http/httptest`, `encoding/json`, `github.com/stretchr/testify`

---

## Chunk 1: TFC Client and Bytes-Based State Loading

### Task 1: Create `pkg/tfc/client.go` with `StatePull`

**Files:**
- Create: `pkg/tfc/client.go`
- Create: `pkg/tfc/client_test.go`

- [ ] **Step 1: Write the failing test for service discovery**

In `pkg/tfc/client_test.go`:

```go
package tfc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMockTFCServer returns an httptest.Server that implements the TFC-compatible
// API endpoints needed by StatePull: well-known discovery, workspace lookup,
// state version, and state download.
func newMockTFCServer(t *testing.T, org, workspace, workspaceID string, stateBody []byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// Service discovery
	mux.HandleFunc("/.well-known/terraform.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"tfe.v2": "/api/v2/",
		})
	})

	// Workspace lookup
	mux.HandleFunc(fmt.Sprintf("/api/v2/organizations/%s/workspaces/%s", org, workspace),
		func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer test-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.api+json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"id":   workspaceID,
					"type": "workspaces",
				},
			})
		})

	// State version
	mux.HandleFunc(fmt.Sprintf("/api/v2/workspaces/%s/current-state-version", workspaceID),
		func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer test-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.api+json")
			// Use the test server's own URL for the download link
			downloadURL := fmt.Sprintf("%s/state-download/%s", r.Header.Get("X-Test-Base-URL"), workspaceID)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"type": "state-versions",
					"attributes": map[string]interface{}{
						"hosted-state-download-url": downloadURL,
					},
				},
			})
		})

	// State download
	mux.HandleFunc(fmt.Sprintf("/state-download/%s", workspaceID),
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(stateBody)
		})

	return httptest.NewServer(mux)
}

func TestStatePull_Success(t *testing.T) {
	t.Parallel()

	fakeState := []byte(`{"version":4,"terraform_version":"1.5.0","serial":1,"lineage":"abc","outputs":{},"resources":[]}`)
	server := newMockTFCServer(t, "myorg", "myworkspace", "ws-abc123", fakeState)
	defer server.Close()

	// Inject the server URL into requests so the state-version handler can build download URLs
	client := &Client{
		Hostname: server.URL,
		Token:    "test-token",
		HTTP: &http.Client{
			Transport: &addBaseURLHeader{base: server.URL, rt: http.DefaultTransport},
		},
	}

	data, err := client.StatePull(context.Background(), "myorg", "myworkspace")
	require.NoError(t, err)
	assert.Equal(t, fakeState, data)
}

// addBaseURLHeader is a test RoundTripper that injects X-Test-Base-URL header
// so the mock server can construct absolute download URLs.
type addBaseURLHeader struct {
	base string
	rt   http.RoundTripper
}

func (a *addBaseURLHeader) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("X-Test-Base-URL", a.base)
	return a.rt.RoundTrip(req)
}

func TestStatePull_Unauthorized(t *testing.T) {
	t.Parallel()

	server := newMockTFCServer(t, "myorg", "myworkspace", "ws-abc123", nil)
	defer server.Close()

	client := &Client{
		Hostname: server.URL,
		Token:    "wrong-token",
	}

	_, err := client.StatePull(context.Background(), "myorg", "myworkspace")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
}

func TestStatePull_WorkspaceNotFound(t *testing.T) {
	t.Parallel()

	server := newMockTFCServer(t, "myorg", "myworkspace", "ws-abc123", nil)
	defer server.Close()

	client := &Client{
		Hostname: server.URL,
		Token:    "test-token",
		HTTP: &http.Client{
			Transport: &addBaseURLHeader{base: server.URL, rt: http.DefaultTransport},
		},
	}

	_, err := client.StatePull(context.Background(), "myorg", "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go test ./pkg/tfc/ -v -run TestStatePull`
Expected: compilation error — `pkg/tfc` package does not exist

- [ ] **Step 3: Implement `pkg/tfc/client.go`**

```go
// Copyright 2016-2025, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tfc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client is a minimal HTTP client for TFC-compatible APIs (TFC, TFE, Scalr).
type Client struct {
	Hostname string         // e.g. "veridos-america.scalr.io" (no scheme) or "http://localhost:PORT" for tests
	Token    string
	HTTP     *http.Client
}

// StatePull downloads the current state for a workspace from a TFC-compatible API.
func (c *Client) StatePull(ctx context.Context, org, workspace string) ([]byte, error) {
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	baseURL := c.baseURL()

	// Step 1: Service discovery
	apiPrefix, err := c.discover(ctx, httpClient, baseURL)
	if err != nil {
		return nil, err
	}

	// Step 2: Workspace lookup
	wsID, err := c.getWorkspaceID(ctx, httpClient, apiPrefix, org, workspace)
	if err != nil {
		return nil, err
	}

	// Step 3: Get state download URL
	downloadURL, err := c.getStateDownloadURL(ctx, httpClient, apiPrefix, wsID)
	if err != nil {
		return nil, err
	}

	// Step 4: Download state
	return c.downloadState(ctx, httpClient, downloadURL)
}

func (c *Client) baseURL() string {
	h := c.Hostname
	if strings.HasPrefix(h, "http://") || strings.HasPrefix(h, "https://") {
		return strings.TrimRight(h, "/")
	}
	return "https://" + strings.TrimRight(h, "/")
}

func (c *Client) discover(ctx context.Context, httpClient *http.Client, baseURL string) (string, error) {
	url := baseURL + "/.well-known/terraform.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating discovery request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("service discovery failed for %s: %w", c.Hostname, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("service discovery failed: %s returned status %d", c.Hostname, resp.StatusCode)
	}

	var discovery map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return "", fmt.Errorf("service discovery failed: %s did not return a valid /.well-known/terraform.json", c.Hostname)
	}

	// Try tfe.v2 first, then state.v2
	for _, key := range []string{"tfe.v2", "state.v2"} {
		if prefix, ok := discovery[key]; ok {
			prefix = strings.TrimRight(prefix, "/")
			return baseURL + "/" + strings.TrimLeft(prefix, "/"), nil
		}
	}

	return "", fmt.Errorf("service discovery failed: %s did not return a valid /.well-known/terraform.json", c.Hostname)
}

func (c *Client) doJSON(ctx context.Context, httpClient *http.Client, url string, target interface{}) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/vnd.api+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return resp, fmt.Errorf("unexpected response from %s: %d", url, resp.StatusCode)
	}

	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return resp, fmt.Errorf("unexpected response from %s: invalid JSON", url)
	}
	return resp, nil
}

func (c *Client) getWorkspaceID(ctx context.Context, httpClient *http.Client, apiPrefix, org, workspace string) (string, error) {
	url := fmt.Sprintf("%s/organizations/%s/workspaces/%s", apiPrefix, org, workspace)

	var result struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	resp, err := c.doJSON(ctx, httpClient, url, &result)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			return "", fmt.Errorf("authentication failed for %s", c.Hostname)
		}
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return "", fmt.Errorf("workspace %s/%s not found on %s", org, workspace, c.Hostname)
		}
		return "", fmt.Errorf("looking up workspace %s/%s: %w", org, workspace, err)
	}

	if result.Data.ID == "" {
		return "", fmt.Errorf("workspace %s/%s returned empty ID", org, workspace)
	}

	return result.Data.ID, nil
}

func (c *Client) getStateDownloadURL(ctx context.Context, httpClient *http.Client, apiPrefix, workspaceID string) (string, error) {
	url := fmt.Sprintf("%s/workspaces/%s/current-state-version", apiPrefix, workspaceID)

	var result struct {
		Data struct {
			Attributes struct {
				HostedStateDownloadURL string `json:"hosted-state-download-url"`
			} `json:"attributes"`
		} `json:"data"`
	}

	resp, err := c.doJSON(ctx, httpClient, url, &result)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return "", fmt.Errorf("no state found for workspace %s", workspaceID)
		}
		return "", fmt.Errorf("getting state version for workspace %s: %w", workspaceID, err)
	}

	downloadURL := result.Data.Attributes.HostedStateDownloadURL
	if downloadURL == "" {
		return "", fmt.Errorf("no state download URL for workspace %s", workspaceID)
	}

	return downloadURL, nil
}

func (c *Client) downloadState(ctx context.Context, httpClient *http.Client, downloadURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating state download request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading state: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading state: status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
```

- [ ] **Step 4: Run the tests**

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go test ./pkg/tfc/ -v -run TestStatePull`
Expected: all 3 tests PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate
git add pkg/tfc/client.go pkg/tfc/client_test.go
git commit -m "feat(tfc): add TFC-compatible API client with StatePull"
```

---

### Task 2: Refactor `DetectStateFormat` and `LoadRawState` to accept `[]byte`

**Files:**
- Modify: `pkg/tofu_eval.go:54-69` (DetectStateFormat)
- Modify: `pkg/tofu_eval.go:143-154` (LoadRawState)
- Modify: `pkg/generate_module_map.go:42-43` (caller of DetectStateFormat)
- Modify: `pkg/generate_module_map.go:54` (caller of LoadRawState)
- Modify: `pkg/tofu_eval_test.go:33-45` (existing tests)

- [ ] **Step 1: Update existing tests to use bytes-based functions**

In `pkg/tofu_eval_test.go`, update `TestDetectStateFormat_RawTfstate` and `TestDetectStateFormat_TofuShowJson` to read the file into bytes first, then call `DetectStateFormatBytes`. Update `TestLoadRawState` similarly to call `LoadRawStateBytes`.

```go
func TestDetectStateFormat_RawTfstate(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("testdata", "tofu_tfstate_indexed_modules.tfstate"))
	require.NoError(t, err)
	format, err := DetectStateFormatBytes(data)
	require.NoError(t, err)
	assert.Equal(t, StateFormatRaw, format)
}

func TestDetectStateFormat_TofuShowJson(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("testdata", "tofu_state_indexed_modules.json"))
	require.NoError(t, err)
	format, err := DetectStateFormatBytes(data)
	require.NoError(t, err)
	assert.Equal(t, StateFormatTofuShowJSON, format)
}

func TestLoadRawState(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("testdata", "tofu_tfstate_indexed_modules.tfstate"))
	require.NoError(t, err)
	state, err := LoadRawStateBytes(data)
	require.NoError(t, err)
	require.NotNil(t, state)

	mod0 := state.Module(addrs.RootModuleInstance.Child("pet", addrs.IntKey(0)))
	require.NotNil(t, mod0, "expected module.pet[0] to exist in state")
	assert.Greater(t, len(mod0.Resources), 0, "expected module.pet[0] to have resources")

	mod1 := state.Module(addrs.RootModuleInstance.Child("pet", addrs.IntKey(1)))
	require.NotNil(t, mod1, "expected module.pet[1] to exist in state")
	assert.Greater(t, len(mod1.Resources), 0, "expected module.pet[1] to have resources")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go test ./pkg/ -v -run "TestDetectStateFormat|TestLoadRawState" -count=1`
Expected: compilation error — `DetectStateFormatBytes` and `LoadRawStateBytes` not defined

- [ ] **Step 3: Implement the bytes-based functions**

In `pkg/tofu_eval.go`, rename existing functions and add file-based wrappers:

Replace the existing `DetectStateFormat` function (lines 54-69) with:

```go
// DetectStateFormatBytes auto-detects whether the data is a raw .tfstate
// or the JSON output of `tofu show -json`.
func DetectStateFormatBytes(data []byte) (StateFormat, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return 0, fmt.Errorf("parsing state as JSON: %w", err)
	}

	if _, hasFormatVersion := top["format_version"]; hasFormatVersion {
		return StateFormatTofuShowJSON, nil
	}
	return StateFormatRaw, nil
}

// DetectStateFormat reads a state file from disk and detects its format.
func DetectStateFormat(path string) (StateFormat, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("reading state file %s: %w", path, err)
	}
	return DetectStateFormatBytes(data)
}
```

Replace the existing `LoadRawState` function (lines 143-154) with:

```go
// LoadRawStateBytes parses raw .tfstate bytes into a *states.State.
func LoadRawStateBytes(data []byte) (*states.State, error) {
	sf, err := statefile.Read(bytes.NewReader(data), encryption.StateEncryptionDisabled())
	if err != nil {
		return nil, fmt.Errorf("parsing state: %w", err)
	}
	return sf.State, nil
}

// LoadRawState reads a raw .tfstate file from disk.
func LoadRawState(path string) (*states.State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading state file %s: %w", path, err)
	}
	return LoadRawStateBytes(data)
}
```

- [ ] **Step 4: Run all tests to verify nothing broke**

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go test ./pkg/ -v -run "TestDetectStateFormat|TestLoadRawState|TestEvaluate" -count=1`
Expected: all existing tests PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate
git add pkg/tofu_eval.go pkg/tofu_eval_test.go
git commit -m "refactor: extract bytes-based DetectStateFormatBytes and LoadRawStateBytes"
```

---

### Task 3: Wire bytes-based functions into `GenerateModuleMap`

**Files:**
- Modify: `pkg/generate_module_map.go:34` (signature)
- Modify: `pkg/generate_module_map.go:42-100` (state loading logic)
- Modify: `cmd/module_map.go:56` (call site)

- [ ] **Step 1: Update `GenerateModuleMap` to use bytes-based loading and accept `*RemoteStateOptions`**

In `pkg/generate_module_map.go`, add the `RemoteStateOptions` struct and update `GenerateModuleMap`:

Add after the imports:

```go
// RemoteStateOptions configures pulling state from a TFC-compatible API.
type RemoteStateOptions struct {
	Hostname     string
	Organization string
	Workspace    string
	Token        string
}
```

Replace the `GenerateModuleMap` function signature and state-loading logic (lines 34-101) with:

```go
func GenerateModuleMap(ctx context.Context, tfDir, stateFilePath, outputPath, stackName, projectName string, remote *RemoteStateOptions) error {
	if stateFilePath != "" && remote != nil {
		return fmt.Errorf("stateFilePath and remote are mutually exclusive")
	}

	// Step 1: Load Terraform/OpenTofu configuration.
	config, err := LoadConfig(tfDir)
	if err != nil {
		return fmt.Errorf("loading config from %s: %w", tfDir, err)
	}

	// Step 2: Load state bytes.
	var stateData []byte
	if remote != nil {
		tfcClient := &tfcpkg.Client{
			Hostname: remote.Hostname,
			Token:    remote.Token,
		}
		stateData, err = tfcClient.StatePull(ctx, remote.Organization, remote.Workspace)
		if err != nil {
			return fmt.Errorf("pulling remote state: %w", err)
		}
	} else {
		stateData, err = os.ReadFile(stateFilePath)
		if err != nil {
			return fmt.Errorf("reading state file %s: %w", stateFilePath, err)
		}
	}

	// Step 3: Detect format and parse.
	format, err := DetectStateFormatBytes(stateData)
	if err != nil {
		return fmt.Errorf("detecting state format: %w", err)
	}

	var rawState *states.State
	var tofuCtx *tofu.Context
	var pulumiProviders map[providermap.TerraformProviderName]*ProviderWithMetadata

	switch format {
	case StateFormatRaw:
		rawState, err = LoadRawStateBytes(stateData)
		if err != nil {
			return fmt.Errorf("loading raw state: %w", err)
		}

		var cleanup func()
		tofuCtx, cleanup, err = Evaluate(config, rawState, tfDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not create evaluation context: %v\n", err)
			fmt.Fprintf(os.Stderr, "Continuing without evaluated values.\n")
			tofuCtx = nil
		}
		if cleanup != nil {
			defer cleanup()
		}

		tfProviders := getTerraformProvidersForRawState(rawState)
		pulumiProviders, err = PulumiProvidersForTerraformProviders(tfProviders, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not resolve Pulumi providers: %v\n", err)
			fmt.Fprintf(os.Stderr, "Continuing without Pulumi URNs (will use raw Terraform addresses).\n")
			pulumiProviders = nil
		}

	case StateFormatTofuShowJSON:
		var tfjsonState tfjson.State
		if err := json.Unmarshal(stateData, &tfjsonState); err != nil {
			return fmt.Errorf("parsing tofu show JSON state: %w", err)
		}

		rawState = rawStateFromTfjson(&tfjsonState)

		pulumiProviders, err = GetPulumiProvidersForTerraformState(&tfjsonState, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not resolve Pulumi providers: %v\n", err)
			fmt.Fprintf(os.Stderr, "Continuing without Pulumi URNs (will use raw Terraform addresses).\n")
			pulumiProviders = nil
		}
	}

	// Steps 5-7 below are unchanged from the original function (lines 103-124).

	// Step 5: Build sensitivity map from provider schemas.
	sensitivityMap, err := BuildSensitivityMap(ctx, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not build sensitivity map: %v\n", err)
		fmt.Fprintf(os.Stderr, "Continuing without attribute redaction.\n")
		sensitivityMap = nil
	}

	// Step 6: Build the module map.
	mm, err := BuildModuleMap(config, tofuCtx, rawState, pulumiProviders, sensitivityMap, stackName, projectName)
	if err != nil {
		return fmt.Errorf("building module map: %w", err)
	}

	// Step 7: Write the module map to disk.
	if err := WriteModuleMap(mm, outputPath); err != nil {
		return fmt.Errorf("writing module map: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Module map written to %s\n", outputPath)
	return nil
}
```

Update imports: add `tfcpkg "github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tfc"`. Keep the `tofuutil` import — it is still used by `rawStateFromTfjson`.

**Note:** The `StateFormatTofuShowJSON` case now uses `json.Unmarshal` directly instead of `tofuutil.LoadTerraformState`, avoiding any `tofu init` calls. This changes behavior for local `--state-file` with tofu-show-json format too (no longer invokes tofu CLI), which is an improvement.

- [ ] **Step 2: Update the call site in `cmd/module_map.go`**

In `cmd/module_map.go` line 56, pass `nil` for the new remote parameter to preserve existing behavior:

```go
err := pkg.GenerateModuleMap(cmd.Context(), from, stateFile, out, pulumiStack, pulumiProject, nil)
```

- [ ] **Step 3: Verify compilation**

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go build ./...`
Expected: compiles without errors

- [ ] **Step 4: Run existing module-map tests**

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go test ./pkg/ -v -run "TestBuildModuleMap|TestGenerateModuleMap" -count=1`
Expected: all existing tests PASS (if any exist; otherwise confirm no regressions with `go test ./...`)

- [ ] **Step 5: Commit**

```bash
cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate
git add pkg/generate_module_map.go cmd/module_map.go
git commit -m "refactor: wire bytes-based state loading and RemoteStateOptions into GenerateModuleMap"
```

---

## Chunk 2: CLI Flags and Integration

### Task 4: Add remote flags to `module-map` command

**Files:**
- Modify: `cmd/module_map.go`

- [ ] **Step 1: Add new flags and validation logic**

Replace the entire `newModuleMapCmd` function in `cmd/module_map.go`. **Critical:** The existing `cmd.MarkFlagRequired("state-file")` on line 71 must be removed — `--state-file` is no longer required. The replacement code below omits it and adds custom validation in `RunE` instead:

```go
func newModuleMapCmd() *cobra.Command {
	var from string
	var stateFile string
	var out string
	var pulumiStack string
	var pulumiProject string
	var hostname string
	var organization string
	var workspace string
	var tokenEnv string

	cmd := &cobra.Command{
		Use:   "module-map",
		Short: "Generate a module-map.json sidecar from Terraform sources and state",
		Long: `Generate a module-map.json sidecar file that describes Terraform module
instances, their interfaces (inputs/outputs), and the Pulumi URNs of
resources belonging to each module instance.

State can be provided as a local file (--state-file) or pulled from a
TFC-compatible remote backend (--hostname, --organization, --workspace,
--token-env).

Examples:

  # From a local state file
  pulumi-terraform-migrate module-map \
    --from path/to/terraform-sources \
    --state-file path/to/terraform.tfstate \
    --out /tmp/module-map.json \
    --pulumi-stack dev \
    --pulumi-project myproject

  # From a TFC-compatible remote (Scalr, TFC, TFE)
  pulumi-terraform-migrate module-map \
    --from path/to/terraform-sources \
    --hostname veridos-america.scalr.io \
    --organization valid \
    --workspace dmvhm-infrastructure-develop \
    --token-env SCALR_TOKEN \
    --out /tmp/module-map.json \
    --pulumi-stack dev \
    --pulumi-project myproject
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate mutually exclusive state sources.
			remoteFlags := []string{"hostname", "organization", "workspace", "token-env"}
			remoteValues := map[string]string{
				"hostname":     hostname,
				"organization": organization,
				"workspace":    workspace,
				"token-env":    tokenEnv,
			}
			hasAnyRemote := false
			for _, v := range remoteValues {
				if v != "" {
					hasAnyRemote = true
					break
				}
			}

			if stateFile != "" && hasAnyRemote {
				return fmt.Errorf("--state-file and remote flags (--hostname, --organization, --workspace, --token-env) are mutually exclusive")
			}

			if stateFile == "" && !hasAnyRemote {
				return fmt.Errorf("either --state-file or all remote flags (--hostname, --organization, --workspace, --token-env) must be provided")
			}

			var remote *pkg.RemoteStateOptions
			if hasAnyRemote {
				var missing []string
				for _, flag := range remoteFlags {
					if remoteValues[flag] == "" {
						missing = append(missing, "--"+flag)
					}
				}
				if len(missing) > 0 {
					return fmt.Errorf("--hostname, --organization, --workspace, and --token-env are all required when using remote state (missing: %s)",
						strings.Join(missing, ", "))
				}

				token := os.Getenv(tokenEnv)
				if token == "" {
					return fmt.Errorf("environment variable %s is empty or not set", tokenEnv)
				}

				remote = &pkg.RemoteStateOptions{
					Hostname:     hostname,
					Organization: organization,
					Workspace:    workspace,
					Token:        token,
				}
			}

			err := pkg.GenerateModuleMap(cmd.Context(), from, stateFile, out, pulumiStack, pulumiProject, remote)
			if err != nil {
				// Enrich authentication errors with the env var name for user guidance.
				if remote != nil && strings.Contains(err.Error(), "authentication failed") {
					return fmt.Errorf("%w: check token in env var %s", err, tokenEnv)
				}
				return fmt.Errorf("failed to generate module map: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&from, "from", "f", "", "Path to the Terraform root folder")
	cmd.Flags().StringVar(&stateFile, "state-file", "", "Path to terraform.tfstate or tofu show -json output")
	cmd.Flags().StringVarP(&out, "out", "o", "", "Where to emit the module-map.json file")
	cmd.Flags().StringVar(&pulumiStack, "pulumi-stack", "", "Pulumi stack name for URN generation")
	cmd.Flags().StringVar(&pulumiProject, "pulumi-project", "", "Pulumi project name for URN generation")
	cmd.Flags().StringVar(&hostname, "hostname", "", "TFC-compatible API hostname (e.g. app.terraform.io)")
	cmd.Flags().StringVar(&organization, "organization", "", "Organization name on the TFC-compatible host")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace name on the TFC-compatible host")
	cmd.Flags().StringVar(&tokenEnv, "token-env", "", "Name of environment variable containing the API token")

	cmd.MarkFlagRequired("from")
	cmd.MarkFlagRequired("out")
	cmd.MarkFlagRequired("pulumi-stack")
	cmd.MarkFlagRequired("pulumi-project")

	return cmd
}
```

Add `"os"` and `"strings"` to the imports.

- [ ] **Step 2: Verify compilation**

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go build ./...`
Expected: compiles without errors

- [ ] **Step 3: Test CLI validation manually**

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go run . module-map --from /tmp --out /tmp/out.json --pulumi-stack dev --pulumi-project test 2>&1`
Expected: error message containing `"either --state-file or all remote flags"`

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go run . module-map --from /tmp --out /tmp/out.json --pulumi-stack dev --pulumi-project test --hostname foo --state-file bar 2>&1`
Expected: error message containing `"mutually exclusive"`

Run: `cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate && go run . module-map --from /tmp --out /tmp/out.json --pulumi-stack dev --pulumi-project test --hostname foo --organization bar 2>&1`
Expected: error message containing `"missing: --workspace, --token-env"`

- [ ] **Step 4: Commit**

```bash
cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate
git add cmd/module_map.go
git commit -m "feat(module-map): add --hostname, --organization, --workspace, --token-env flags for remote state"
```

---

### Task 5: End-to-end test with real remote (manual verification)

This is a manual smoke test against Scalr to confirm the full pipeline works.

- [ ] **Step 1: Build the binary**

```bash
cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate
go build -o pulumi-tool-terraform-migrate ./
```

- [ ] **Step 2: Source the Scalr token from ESC**

The user should run their ESC env to export `SCALR_TOKEN` (or whichever env var they use).

- [ ] **Step 3: Run module-map against dmvhm develop**

```bash
cd /Users/jdavenport/pulumi-repos/pulumi-tool-terraform-migrate
./pulumi-tool-terraform-migrate module-map \
  --from /Users/jdavenport/pulumi-repos/veridos/dmvhm-infrastructure/environments/develop \
  --hostname veridos-america.scalr.io \
  --organization valid \
  --workspace dmvhm-infrastructure-develop \
  --token-env SCALR_TOKEN \
  --out /tmp/dmvhm-develop-module-map.json \
  --pulumi-stack dev \
  --pulumi-project veridos
```

Expected: `module-map.json` written to `/tmp/dmvhm-develop-module-map.json` with module entries for vpc, rds, front-end, waf, etc.

- [ ] **Step 4: Spot-check the output**

Verify the JSON has a `"modules"` key with entries like `"caas_rds"`, `"cf_rds"`, `"capture_ui"`, etc. Each should have `resources`, `interface.inputs`, and `interface.outputs`.
