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

package pkg

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pulumi/opentofu/addrs"
	"github.com/pulumi/opentofu/states"
	"github.com/pulumi/opentofu/tofu"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/providermap"
	tfcpkg "github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tfc"
	tofuutil "github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tofu"
)

// RemoteStateOptions configures pulling state from a TFC-compatible API.
type RemoteStateOptions struct {
	Hostname     string
	Organization string
	Workspace    string
	Token        string
}

// GenerateModuleMap is the top-level orchestrator for the module-map subcommand.
// It loads Terraform configuration and state, resolves Pulumi providers, builds a
// ModuleMap, and writes it to outputPath.
func GenerateModuleMap(ctx context.Context, tfDir, stateFilePath, outputPath, stackName, projectName string, remote *RemoteStateOptions) error {
	if stateFilePath != "" && remote != nil {
		return fmt.Errorf("stateFilePath and remote are mutually exclusive")
	}

	// Step 1: Load Terraform/OpenTofu configuration.
	fmt.Fprintf(os.Stderr, "[1/7] Loading Terraform configuration from %s...\n", tfDir)
	config, err := LoadConfig(tfDir)
	if err != nil {
		return fmt.Errorf("loading config from %s: %w", tfDir, err)
	}

	// Step 2: Load state bytes.
	var stateData []byte
	if remote != nil {
		fmt.Fprintf(os.Stderr, "[2/7] Pulling state from %s (%s/%s)...\n", remote.Hostname, remote.Organization, remote.Workspace)
		tfcClient := &tfcpkg.Client{
			Hostname: remote.Hostname,
			Token:    remote.Token,
		}
		stateData, err = tfcClient.StatePull(ctx, remote.Organization, remote.Workspace)
		if err != nil {
			return fmt.Errorf("pulling remote state: %w", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "[2/7] Reading state from %s...\n", stateFilePath)
		stateData, err = os.ReadFile(stateFilePath)
		if err != nil {
			return fmt.Errorf("reading state file %s: %w", stateFilePath, err)
		}
	}

	// Step 3: Detect format and parse.
	fmt.Fprintf(os.Stderr, "[3/7] Detecting state format...\n")
	format, err := DetectStateFormatBytes(stateData)
	if err != nil {
		return fmt.Errorf("detecting state format: %w", err)
	}

	var rawState *states.State
	var tofuCtx *tofu.Context
	var pulumiProviders map[providermap.TerraformProviderName]*ProviderWithMetadata

	switch format {
	case StateFormatRaw:
		fmt.Fprintf(os.Stderr, "[4/7] Parsing raw state and evaluating expressions...\n")
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

		fmt.Fprintf(os.Stderr, "[4b/7] Resolving Pulumi providers...\n")
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

	// Step 5: Build sensitivity map from provider schemas.
	fmt.Fprintf(os.Stderr, "[5/7] Building sensitivity map...\n")
	sensitivityMap, err := BuildSensitivityMap(ctx, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not build sensitivity map: %v\n", err)
		fmt.Fprintf(os.Stderr, "Continuing without attribute redaction.\n")
		sensitivityMap = nil
	}

	// Step 6: Build the module map.
	fmt.Fprintf(os.Stderr, "[6/7] Building module map...\n")
	mm, err := BuildModuleMap(config, tofuCtx, rawState, pulumiProviders, sensitivityMap, stackName, projectName)
	if err != nil {
		return fmt.Errorf("building module map: %w", err)
	}

	// Step 7: Write the module map to disk.
	fmt.Fprintf(os.Stderr, "[7/7] Writing module map to %s...\n", outputPath)
	if err := WriteModuleMap(mm, outputPath); err != nil {
		return fmt.Errorf("writing module map: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Module map written to %s\n", outputPath)
	return nil
}

// rawStateFromTfjson builds a synthetic *states.State from a tfjson.State.
// This allows the StateFormatTofuShowJSON path to reuse the same BuildModuleMap
// code that works with raw state.
func rawStateFromTfjson(tfjsonState *tfjson.State) *states.State {
	state := states.NewState()

	tofuutil.VisitResources(tfjsonState, func(r *tfjson.StateResource) error {
		// Parse module address from the resource address.
		segments := parseModuleSegments(r.Address)
		moduleAddr := addrs.RootModuleInstance
		for _, seg := range segments {
			if seg.key == "" {
				moduleAddr = moduleAddr.Child(seg.name, addrs.NoKey)
			} else if _, err := fmt.Sscanf(seg.key, "%d", new(int)); err == nil {
				var idx int
				fmt.Sscanf(seg.key, "%d", &idx)
				moduleAddr = moduleAddr.Child(seg.name, addrs.IntKey(idx))
			} else {
				moduleAddr = moduleAddr.Child(seg.name, addrs.StringKey(seg.key))
			}
		}

		// Parse provider.
		provider, _ := addrs.ParseProviderSourceString(r.ProviderName)
		providerConfig := addrs.AbsProviderConfig{
			Provider: provider,
		}

		// Build resource address.
		mode := addrs.ManagedResourceMode
		if r.Mode == tfjson.DataResourceMode {
			mode = addrs.DataResourceMode
		}
		resAddr := addrs.Resource{
			Mode: mode,
			Type: r.Type,
			Name: r.Name,
		}

		// Serialize attribute values to JSON.
		attrsJSON, _ := json.Marshal(r.AttributeValues)

		module := state.EnsureModule(moduleAddr)
		module.SetResourceProvider(resAddr, providerConfig)
		module.SetResourceInstanceCurrent(
			addrs.ResourceInstance{Resource: resAddr, Key: addrs.NoKey},
			&states.ResourceInstanceObjectSrc{AttrsJSON: attrsJSON},
			providerConfig,
			nil,
		)

		return nil
	}, &tofuutil.VisitOptions{IncludeDataSources: true})

	return state
}
