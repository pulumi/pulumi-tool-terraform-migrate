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

// ListVariables fetches all Terraform variables from the workspace.
func (c *Client) ListVariables(ctx context.Context, org, workspace string) ([]WorkspaceVariable, error) {
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

	// Fetch all vars in a single request. Scalr's pagination is unreliable
	// (page[number] parameter is ignored), so we request a large page size.
	url := fmt.Sprintf("%s/workspaces/%s/vars?page%%5Bsize%%5D=500", apiPrefix, wsID)

	var result struct {
		Data []struct {
			Attributes struct {
				Key       string `json:"key"`
				Value     string `json:"value"`
				Category  string `json:"category"`
				HCL       bool   `json:"hcl"`
				Sensitive bool   `json:"sensitive"`
			} `json:"attributes"`
		} `json:"data"`
	}

	_, err = c.doJSON(ctx, httpClient, url, &result)
	if err != nil {
		return nil, fmt.Errorf("listing workspace variables: %w", err)
	}

	var allVars []WorkspaceVariable
	for _, d := range result.Data {
		if d.Attributes.Category != "terraform" {
			continue
		}
		allVars = append(allVars, WorkspaceVariable{
			Key:       d.Attributes.Key,
			Value:     d.Attributes.Value,
			Category:  d.Attributes.Category,
			HCL:       d.Attributes.HCL,
			Sensitive: d.Attributes.Sensitive,
		})
	}

	return allVars, nil
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
