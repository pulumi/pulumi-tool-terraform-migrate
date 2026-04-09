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
	"fmt"
	"os"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pulumi/opentofu/states"
	tofuutil "github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tofu"
	"github.com/pulumi/opentofu/tofu"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/providermap"
)

// GenerateModuleMap is the top-level orchestrator for the module-map subcommand.
// It loads Terraform configuration and state, resolves Pulumi providers, builds a
// ModuleMap, and writes it to outputPath.
func GenerateModuleMap(ctx context.Context, tfDir, stateFilePath, outputPath, stackName, projectName string) error {
	// Step 1: Load Terraform/OpenTofu configuration.
	config, err := LoadConfig(tfDir)
	if err != nil {
		return fmt.Errorf("loading config from %s: %w", tfDir, err)
	}

	// Step 2: Detect state file format.
	format, err := DetectStateFormat(stateFilePath)
	if err != nil {
		return fmt.Errorf("detecting state format: %w", err)
	}

	var rawState *states.State
	var tofuCtx *tofu.Context
	var tfjsonState *tfjson.State
	var pulumiProviders map[providermap.TerraformProviderName]*ProviderWithMetadata

	switch format {
	case StateFormatRaw:
		// Load raw tfstate for evaluation.
		rawState, err = LoadRawState(stateFilePath)
		if err != nil {
			return fmt.Errorf("loading raw state: %w", err)
		}

		// Create tofu context for expression evaluation (graceful degradation).
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

		// For resource matching we need tfjson state. Load it via the tofu loader
		// which handles .json files directly and runs tofu show -json for .tfstate files.
		tfjsonState, err = tofuutil.LoadTerraformState(ctx, tofuutil.LoadTerraformStateOptions{
			StateFilePath: stateFilePath,
			ProjectDir:    tfDir,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load tfjson state for resource matching: %v\n", err)
			fmt.Fprintf(os.Stderr, "Continuing without resource URNs.\n")
			tfjsonState = nil
		}

	case StateFormatTofuShowJSON:
		// Load tofu show -json output directly.
		tfjsonState, err = tofuutil.LoadTerraformState(ctx, tofuutil.LoadTerraformStateOptions{
			StateFilePath: stateFilePath,
		})
		if err != nil {
			return fmt.Errorf("loading tofu show JSON state: %w", err)
		}
		// No raw state or evaluation context available for show-json format.
	}

	// Step 5: Resolve Pulumi providers for URN generation (graceful degradation).
	if tfjsonState != nil {
		pulumiProviders, err = GetPulumiProvidersForTerraformState(tfjsonState, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not resolve Pulumi providers: %v\n", err)
			fmt.Fprintf(os.Stderr, "Continuing without Pulumi URNs (will use raw Terraform addresses).\n")
			pulumiProviders = nil
		}
	}

	// Step 6: Build the module map.
	mm, err := BuildModuleMap(config, tofuCtx, rawState, tfjsonState, pulumiProviders, stackName, projectName)
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
