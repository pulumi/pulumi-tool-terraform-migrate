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
	"os"
	"strings"
	"time"
)

type Client struct {
	Hostname string
	Token    string
	HTTP     *http.Client
}

// WorkspaceVariable represents a single variable from the TFC/Scalr workspace.
type WorkspaceVariable struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Category  string `json:"category"`  // "terraform" or "env"
	HCL       bool   `json:"hcl"`
	Sensitive bool   `json:"sensitive"`
}

// ListVariables fetches all Terraform variables from the workspace,
// following JSON:API pagination.
//
// When the backend advertises the Scalr-native IACP v3 API, that endpoint is
// used because it returns all variables (including environment-scope vars).
// The TFE-compatible endpoint only returns workspace-scope variables.
func (c *Client) ListVariables(ctx context.Context, org, workspace string) ([]WorkspaceVariable, error) {
	httpClient := c.httpClient()
	baseURL := c.baseURL()

	disc, err := c.discoverAll(ctx, httpClient, baseURL)
	if err != nil {
		return nil, err
	}

	wsID, err := c.getWorkspaceID(ctx, httpClient, disc.apiPrefix, org, workspace)
	if err != nil {
		return nil, err
	}

	// Prefer IACP v3 when available (Scalr) — it includes environment-scope vars.
	if disc.iacpPrefix != "" {
		varsURL := fmt.Sprintf("%s/vars?filter%%5Bworkspace%%5D=%s", disc.iacpPrefix, wsID)
		vars, err := c.listVarsPaginated(ctx, httpClient, varsURL)
		if err == nil {
			return vars, nil
		}
		fmt.Fprintf(os.Stderr, "Warning: IACP vars endpoint failed, falling back to TFE: %v\n", err)
	}

	// Fall back to TFE-compatible path.
	varsURL := fmt.Sprintf("%s/workspaces/%s/vars", disc.apiPrefix, wsID)
	return c.listVarsPaginated(ctx, httpClient, varsURL)
}

// listVarsPaginated fetches workspace variables from the given base URL with pagination.
func (c *Client) listVarsPaginated(ctx context.Context, httpClient *http.Client, baseVarsURL string) ([]WorkspaceVariable, error) {
	var allVars []WorkspaceVariable
	seen := make(map[string]bool)

	for pageNum := 1; ; pageNum++ {
		sep := "?"
		if strings.Contains(baseVarsURL, "?") {
			sep = "&"
		}
		url := fmt.Sprintf("%s%spage%%5Bnumber%%5D=%d&page%%5Bsize%%5D=100", baseVarsURL, sep, pageNum)

		var page varsPage
		_, err := c.doJSON(ctx, httpClient, url, &page)
		if err != nil {
			return nil, fmt.Errorf("listing workspace variables: %w", err)
		}

		for _, d := range page.Data {
			if d.Attributes.Category != "terraform" {
				continue
			}
			if seen[d.Attributes.Key] {
				continue
			}
			seen[d.Attributes.Key] = true
			allVars = append(allVars, WorkspaceVariable{
				Key:       d.Attributes.Key,
				Value:     d.Attributes.Value,
				Category:  d.Attributes.Category,
				HCL:       d.Attributes.HCL,
				Sensitive: d.Attributes.Sensitive,
			})
		}

		if page.Meta.Pagination.NextPage == nil || pageNum >= page.Meta.Pagination.TotalPages {
			break
		}
	}

	return allVars, nil
}

// varsPage represents one page of the JSON:API list-variables response.
type varsPage struct {
	Data []struct {
		Attributes struct {
			Key       string `json:"key"`
			Value     string `json:"value"`
			Category  string `json:"category"`
			HCL       bool   `json:"hcl"`
			Sensitive bool   `json:"sensitive"`
		} `json:"attributes"`
	} `json:"data"`
	Meta struct {
		Pagination struct {
			CurrentPage int  `json:"current-page"`
			NextPage    *int `json:"next-page"`
			TotalPages  int  `json:"total-pages"`
			TotalCount  int  `json:"total-count"`
		} `json:"pagination"`
	} `json:"meta"`
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			ForceAttemptHTTP2: true,
		},
	}
}

func (c *Client) StatePull(ctx context.Context, org, workspace string) ([]byte, error) {
	httpClient := c.httpClient()

	baseURL := c.baseURL()

	apiPrefix, err := c.discover(ctx, httpClient, baseURL)
	if err != nil {
		return nil, err
	}

	wsID, err := c.getWorkspaceID(ctx, httpClient, apiPrefix, org, workspace)
	if err != nil {
		return nil, err
	}

	downloadURL, err := c.getStateDownloadURL(ctx, httpClient, apiPrefix, wsID)
	if err != nil {
		return nil, err
	}

	return c.downloadState(ctx, httpClient, downloadURL)
}

func (c *Client) baseURL() string {
	h := c.Hostname
	if strings.HasPrefix(h, "http://") || strings.HasPrefix(h, "https://") {
		return strings.TrimRight(h, "/")
	}
	return "https://" + strings.TrimRight(h, "/")
}

// discoveryResult holds the resolved API prefixes from the well-known document.
type discoveryResult struct {
	// apiPrefix is the primary TFE-compatible API prefix (tfe.v2 or state.v2).
	apiPrefix string
	// iacpPrefix is the Scalr-native IACP v3 API prefix, if available.
	// When present, it should be preferred for variable listing as it includes
	// environment-scope variables that the TFE-compatible endpoint omits.
	iacpPrefix string
}

func (c *Client) discover(ctx context.Context, httpClient *http.Client, baseURL string) (string, error) {
	result, err := c.discoverAll(ctx, httpClient, baseURL)
	if err != nil {
		return "", err
	}
	return result.apiPrefix, nil
}

func (c *Client) discoverAll(ctx context.Context, httpClient *http.Client, baseURL string) (*discoveryResult, error) {
	url := baseURL + "/.well-known/terraform.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating discovery request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("service discovery failed for %s: %w", c.Hostname, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("service discovery failed: %s returned status %d", c.Hostname, resp.StatusCode)
	}

	var discovery map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return nil, fmt.Errorf("service discovery failed: %s did not return a valid /.well-known/terraform.json", c.Hostname)
	}

	result := &discoveryResult{}

	for _, key := range []string{"tfe.v2", "state.v2"} {
		if prefix, ok := discovery[key]; ok {
			prefix = strings.TrimRight(prefix, "/")
			result.apiPrefix = baseURL + "/" + strings.TrimLeft(prefix, "/")
			break
		}
	}

	if result.apiPrefix == "" {
		return nil, fmt.Errorf("service discovery failed: %s did not return a valid /.well-known/terraform.json", c.Hostname)
	}

	// Check for Scalr-native IACP v3 API.
	if prefix, ok := discovery["iacp.v3"]; ok {
		prefix = strings.TrimRight(prefix, "/")
		result.iacpPrefix = baseURL + "/" + strings.TrimLeft(prefix, "/")
	}

	return result, nil
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
