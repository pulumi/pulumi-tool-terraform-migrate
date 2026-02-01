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

package statefile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/pulumi/opentofu/addrs"
	"github.com/pulumi/opentofu/states"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/vendored/opentofu/providers"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tfprovider"
)

// StateUpgrader manages TF provider processes for state upgrades.
// Providers are loaded lazily on first use and cached for reuse.
// Call Close() when done to clean up provider processes.
type StateUpgrader struct {
	mu        sync.Mutex
	versions  map[string]string // tfProviderName -> version
	providers map[string]tfprovider.Provider
}

// NewStateUpgrader creates a new upgrader with the specified provider versions.
// The versions map keys should be TF provider addresses (e.g., "registry.terraform.io/hashicorp/aws"
// or just "hashicorp/aws") and values should be version constraints (e.g., "5.0.0").
// Providers are loaded lazily on first use.
func NewStateUpgrader(versions map[string]string) *StateUpgrader {
	return &StateUpgrader{
		versions:  versions,
		providers: make(map[string]tfprovider.Provider),
	}
}

// Close shuts down all cached providers. Should be called when done with state upgrades.
func (p *StateUpgrader) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var errs []error
	for addr, prov := range p.providers {
		if err := prov.Close(); err != nil {
			slog.Warn("Failed to close provider", "addr", addr, "error", err)
			errs = append(errs, fmt.Errorf("close provider %s: %w", addr, err))
		}
	}
	p.providers = make(map[string]tfprovider.Provider)
	return errors.Join(errs...)
}

// getProvider returns a cached provider or loads a new one.
// The version is looked up from the versions map provided at construction time.
func (p *StateUpgrader) getProvider(ctx context.Context, providerAddr string) (tfprovider.Provider, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if prov, ok := p.providers[providerAddr]; ok {
		return prov, nil
	}

	// Look up version from the versions map
	version := p.versions[providerAddr]

	prov, err := tfprovider.LoadProvider(ctx, providerAddr, version)
	if err != nil {
		return nil, err
	}

	p.providers[providerAddr] = prov
	return prov, nil
}

// UpgradeInstance upgrades a resource instance using the TF provider's UpgradeResourceState RPC.
// This handles schema migrations when the state was created with an older provider version
// (e.g., "number" -> "numeric" in the random provider).
//
// The provider version used is determined by the versions map provided at construction time.
//
// Returns a new ResourceInstanceObjectSrc with upgraded attributes, or an error if upgrade fails.
// If no upgrade is needed (schema version is current), returns nil without error.
func (s *StateUpgrader) UpgradeInstance(
	ctx context.Context,
	res *states.Resource,
	key addrs.InstanceKey,
) (*states.ResourceInstanceObjectSrc, error) {
	resourceType := res.Addr.Resource.Type
	providerAddr := res.ProviderConfig.Provider.String()

	ri := res.Instance(key)
	if ri == nil || ri.Current == nil {
		return nil, nil
	}
	instance := ri.Current

	provider, err := s.getProvider(ctx, providerAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to load TF provider %s: %w", providerAddr, err)
	}

	// Get current schema version from provider
	schema := provider.GetProviderSchema()
	if schema.Diagnostics.HasErrors() {
		return nil, fmt.Errorf("failed to get provider schema: %s", schema.Diagnostics.Err())
	}

	resourceSchema, ok := schema.ResourceTypes[resourceType]
	if !ok {
		return nil, fmt.Errorf("resource type %s not found in provider schema", resourceType)
	}

	currentSchemaVersion := uint64(resourceSchema.Version)

	// If already at current version, no upgrade needed
	if instance.SchemaVersion >= currentSchemaVersion {
		return nil, nil
	}

	// Call UpgradeResourceState RPC
	resp := provider.UpgradeResourceState(providers.UpgradeResourceStateRequest{
		TypeName:     resourceType,
		Version:      int64(instance.SchemaVersion),
		RawStateJSON: instance.AttrsJSON,
	})

	if resp.Diagnostics.HasErrors() {
		return nil, fmt.Errorf("UpgradeResourceState failed: %s", resp.Diagnostics.Err())
	}

	if resp.UpgradedState.IsNull() {
		return nil, nil
	}

	// Use CompleteUpgrade to create new instance with upgraded attrs
	upgradedInstance, err := instance.CompleteUpgrade(
		resp.UpgradedState,
		resourceSchema.Block.ImpliedType(),
		currentSchemaVersion,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to complete upgrade: %w", err)
	}

	return upgradedInstance, nil
}
