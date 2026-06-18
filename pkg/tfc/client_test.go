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

func newMockTFCServer(t *testing.T, org, workspace, workspaceID string, stateBody []byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/terraform.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"tfe.v2": "/api/v2/",
		})
	})

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

	mux.HandleFunc(fmt.Sprintf("/api/v2/workspaces/%s/current-state-version", workspaceID),
		func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer test-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.api+json")
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

func TestListVariables_Paginated(t *testing.T) {
	t.Parallel()

	org, workspace, wsID := "myorg", "myworkspace", "ws-abc123"
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/terraform.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"tfe.v2": "/api/v2/"})
	})

	mux.HandleFunc(fmt.Sprintf("/api/v2/organizations/%s/workspaces/%s", org, workspace),
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{"id": wsID, "type": "workspaces"},
			})
		})

	mux.HandleFunc(fmt.Sprintf("/api/v2/workspaces/%s/vars", wsID),
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			if r.URL.Query().Get("page[number]") == "2" {
				// Page 2: last page
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": []map[string]interface{}{
						{"attributes": map[string]interface{}{"key": "var_b", "value": "val_b", "category": "terraform"}},
					},
					"meta": map[string]interface{}{
						"pagination": map[string]interface{}{
							"current-page": 2, "next-page": nil, "total-pages": 2, "total-count": 3,
						},
					},
				})
				return
			}
			// Page 1
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"attributes": map[string]interface{}{"key": "var_a", "value": "val_a", "category": "terraform"}},
					{"attributes": map[string]interface{}{"key": "env_var", "value": "skip", "category": "env"}},
				},
				"meta": map[string]interface{}{
					"pagination": map[string]interface{}{
						"current-page": 1, "next-page": 2, "total-pages": 2, "total-count": 3,
					},
				},
			})
		})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := &Client{Hostname: server.URL, Token: "test-token"}
	vars, err := client.ListVariables(context.Background(), org, workspace)
	require.NoError(t, err)
	require.Len(t, vars, 2)
	assert.Equal(t, "var_a", vars[0].Key)
	assert.Equal(t, "var_b", vars[1].Key)
}

func TestListVariables_SinglePage(t *testing.T) {
	t.Parallel()

	org, workspace, wsID := "myorg", "myworkspace", "ws-abc123"
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/terraform.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"tfe.v2": "/api/v2/"})
	})

	mux.HandleFunc(fmt.Sprintf("/api/v2/organizations/%s/workspaces/%s", org, workspace),
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{"id": wsID, "type": "workspaces"},
			})
		})

	mux.HandleFunc(fmt.Sprintf("/api/v2/workspaces/%s/vars", wsID),
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/vnd.api+json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"attributes": map[string]interface{}{"key": "only_var", "value": "val", "category": "terraform"}},
				},
				"meta": map[string]interface{}{
					"pagination": map[string]interface{}{
						"current-page": 1, "next-page": nil, "total-pages": 1, "total-count": 1,
					},
				},
			})
		})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := &Client{Hostname: server.URL, Token: "test-token"}
	vars, err := client.ListVariables(context.Background(), org, workspace)
	require.NoError(t, err)
	require.Len(t, vars, 1)
	assert.Equal(t, "only_var", vars[0].Key)
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
