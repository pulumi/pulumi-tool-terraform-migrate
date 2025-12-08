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

package bridgedproviders

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge"
	"github.com/pulumi/pulumi/sdk/v3/go/common/diag"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/plugin"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/contract"
	"github.com/pulumi/pulumi/sdk/v3/go/common/workspace"
)

// GetMappingOptions contains options for retrieving provider mappings.
type GetMappingOptions struct {
	// Key is the conversion key for the mapping (e.g., "terraform")
	Key string
	// Provider is an optional source provider key (e.g., "aws" for terraform-aws)
	Provider string
}

// GetMappingResult contains the result of a GetMapping call.
type GetMappingResult struct {
	// Provider is the source provider key that this mapping contains data for
	Provider string
	// Data is the mapping data in a format specific to the conversion plugin/source language
	Data []byte
}

// GetMappingFromBinary initializes a Pulumi provider binary at the given path
// and calls GetMapping with the specified options. The provider must implement
// the ResourceProvider gRPC service defined in provider.proto.
//
// This function:
// 1. Starts the provider binary as a plugin process
// 2. Establishes a gRPC connection to it
// 3. Performs a handshake to negotiate protocol features
// 4. Calls GetMapping with the specified key and provider
// 5. Returns the mapping data
//
// The caller is responsible for ensuring the provider binary exists and is executable.
func GetMappingFromBinary(ctx context.Context, binaryPath string, opts GetMappingOptions) (*GetMappingResult, error) {
	if binaryPath == "" {
		return nil, fmt.Errorf("binaryPath is required")
	}
	if opts.Key == "" {
		return nil, fmt.Errorf("Key is required in GetMappingOptions")
	}

	// Create a minimal host implementation for provider initialization
	// We use a nil host since we don't need logging or other host services for GetMapping
	host := &minimalHost{}

	// Create a plugin context for the provider
	pctx, err := plugin.NewContext(ctx, nil, nil, nil, nil, "", nil, false, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create plugin context: %w", err)
	}
	defer func() {
		contract.IgnoreError(pctx.Close())
	}()

	// Load the provider from the binary path
	provider, err := plugin.NewProviderFromPath(host, pctx, binaryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load provider from %s: %w", binaryPath, err)
	}
	defer func() {
		defer contract.IgnoreError(provider.Close())
	}()

	// Call GetMapping on the provider
	resp, err := provider.GetMapping(ctx, plugin.GetMappingRequest{
		Key:      opts.Key,
		Provider: opts.Provider,
	})
	if err != nil {
		return nil, fmt.Errorf("GetMapping failed: %w", err)
	}

	// If no provider was returned, the provider doesn't have a mapping for the given key
	if resp.Provider == "" {
		return nil, fmt.Errorf("provider at %s does not have a mapping for key %q", binaryPath, opts.Key)
	}

	return &GetMappingResult{
		Provider: resp.Provider,
		Data:     resp.Data,
	}, nil
}

// UnmarshalMappingData takes the result from GetMapping and unmarshals it into
// a tfbridge.ProviderInfo using the MarshallableProvider.Unmarshal() method.
// This assumes the mapping data is JSON-encoded MarshallableProvider data,
// which is the standard format returned by pulumi-terraform-bridge providers.
func UnmarshalMappingData(result *GetMappingResult) (*tfbridge.ProviderInfo, error) {
	if result == nil {
		return nil, fmt.Errorf("result is nil")
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("mapping data is empty")
	}

	var marshallable tfbridge.MarshallableProviderInfo
	if err := json.Unmarshal(result.Data, &marshallable); err != nil {
		return nil, fmt.Errorf("failed to unmarshal mapping data: %w", err)
	}

	provider := marshallable.Unmarshal()
	return provider, nil
}

// minimalHost is a minimal implementation of plugin.Host that provides
// just enough functionality to initialize a provider for GetMapping.
type minimalHost struct{}

var _ plugin.Host = (*minimalHost)(nil)

func (h *minimalHost) ServerAddr() string {
	// Return a dummy address - GetMapping doesn't require a real engine connection
	return "127.0.0.1:0"
}

func (h *minimalHost) Log(sev diag.Severity, urn resource.URN, msg string, streamID int32) {
	// No-op: we don't need logging for GetMapping
}

func (h *minimalHost) LogStatus(sev diag.Severity, urn resource.URN, msg string, streamID int32) {
	// No-op: we don't need logging for GetMapping
}

func (h *minimalHost) Analyzer(nm tokens.QName) (plugin.Analyzer, error) {
	return nil, fmt.Errorf("not implemented")
}

func (h *minimalHost) PolicyAnalyzer(name tokens.QName, path string, opts *plugin.PolicyAnalyzerOptions) (plugin.Analyzer, error) {
	return nil, fmt.Errorf("not implemented")
}

func (h *minimalHost) ListAnalyzers() []plugin.Analyzer {
	return nil
}

func (h *minimalHost) Provider(descriptor workspace.PackageDescriptor) (plugin.Provider, error) {
	return nil, fmt.Errorf("not implemented")
}

func (h *minimalHost) CloseProvider(provider plugin.Provider) error {
	return fmt.Errorf("not implemented")
}

func (h *minimalHost) LanguageRuntime(runtime string) (plugin.LanguageRuntime, error) {
	return nil, fmt.Errorf("not implemented")
}

func (h *minimalHost) EnsurePlugins(plugins []workspace.PluginSpec, kinds plugin.Flags) error {
	return fmt.Errorf("not implemented")
}

func (h *minimalHost) ResolvePlugin(spec workspace.PluginSpec) (*workspace.PluginInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (h *minimalHost) GetProjectPlugins() []workspace.ProjectPlugin {
	return nil
}

func (h *minimalHost) SignalCancellation() error {
	return nil
}

func (h *minimalHost) StartDebugging(info plugin.DebuggingInfo) error {
	return fmt.Errorf("not implemented")
}

func (h *minimalHost) AttachDebugger(spec plugin.DebugSpec) bool {
	return false
}

func (h *minimalHost) Close() error {
	return nil
}
