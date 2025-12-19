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

package migration

import (
	"context"
	"fmt"
	"os"
	"strings"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tofu"
)

// CheckError represents a validation error found during migration check
type CheckError struct {
	Category   string
	Message    string
	Suggestion string
}

// CheckResult contains all validation errors found
type CheckResult struct {
	Errors []CheckError
}

// HasErrors returns true if there are any validation errors
func (cr *CheckResult) HasErrors() bool {
	return len(cr.Errors) > 0
}

// AddError adds a new error to the result
func (cr *CheckResult) AddError(category, message string) {
	cr.Errors = append(cr.Errors, CheckError{
		Category: category,
		Message:  message,
	})
}

// AddErrorWithSuggestion adds a new error with a suggestion to the result
func (cr *CheckResult) AddErrorWithSuggestion(category, message, suggestion string) {
	cr.Errors = append(cr.Errors, CheckError{
		Category:   category,
		Message:    message,
		Suggestion: suggestion,
	})
}

// CheckMigrationIntegrity performs all integrity checks on the migration file
func CheckMigrationIntegrity(ctx context.Context, migrationFile *MigrationFile) (*CheckResult, error) {
	result := &CheckResult{}

	// Check 1: Verify files exist
	checkFilesExist(migrationFile, result)

	// Check 2: Verify unique tf-addr to URN mapping (both directions)
	checkUniqueMapping(migrationFile, result)

	// Check 3: Verify resources match Terraform state
	if err := checkStateConsistency(ctx, migrationFile, result); err != nil {
		return nil, err
	}

	return result, nil
}

// checkFilesExist verifies that all files mentioned in migration.json exist
func checkFilesExist(mf *MigrationFile, result *CheckResult) {
	// Check tf-sources directory
	if _, err := os.Stat(mf.Migration.TFSources); err != nil {
		result.AddError("file-existence", fmt.Sprintf("tf-sources directory does not exist: %s", mf.Migration.TFSources))
	}

	// Check pulumi-sources directory
	if _, err := os.Stat(mf.Migration.PulumiSources); err != nil {
		result.AddError("file-existence", fmt.Sprintf("pulumi-sources directory does not exist: %s", mf.Migration.PulumiSources))
	}

	// Check each stack's files
	for i, stack := range mf.Migration.Stacks {
		stackPrefix := fmt.Sprintf("stack[%d] (%s)", i, stack.PulumiStack)

		// Check tf-state file
		if stack.TFState != "" {
			if _, err := os.Stat(stack.TFState); err != nil {
				result.AddError("file-existence", fmt.Sprintf("%s: tf-state file does not exist: %s", stackPrefix, stack.TFState))
			}
		}
	}
}

// checkUniqueMapping verifies unique bidirectional mapping between tf-addr and URN
func checkUniqueMapping(mf *MigrationFile, result *CheckResult) {
	for i, stack := range mf.Migration.Stacks {
		stackPrefix := fmt.Sprintf("stack[%d] (%s)", i, stack.PulumiStack)

		// Track tf-addr -> URN mapping
		tfAddrToURN := make(map[string][]string)
		// Track URN -> tf-addr mapping
		urnToTFAddr := make(map[string][]string)

		for j, res := range stack.Resources {
			// Validate that resources have both tf-addr and URN
			if res.TFAddr == "" {
				result.AddErrorWithSuggestion("invalid-resource",
					fmt.Sprintf("%s: resource[%d] has empty tf-addr", stackPrefix, j),
					"Remove this invalid resource entry from migration.json")
				continue
			}

			if res.URN == "" && res.Migrate != MigrateModeSkip {
				result.AddErrorWithSuggestion("invalid-resource",
					fmt.Sprintf("%s: resource '%s' has empty URN", stackPrefix, res.TFAddr),
					"Add a URN mapping or set migrate: \"skip\" for this resource")
				continue
			}

			// Skip all resources except those migrating normally.
			if res.Migrate != MigrateModeEmpty {
				continue
			}

			// Record tf-addr -> URN mapping
			tfAddrToURN[res.TFAddr] = append(tfAddrToURN[res.TFAddr], res.URN)

			// Record URN -> tf-addr mapping
			urnToTFAddr[res.URN] = append(urnToTFAddr[res.URN], res.TFAddr)
		}

		// Check for duplicate tf-addr mappings
		for tfAddr, urns := range tfAddrToURN {
			if len(urns) > 1 {
				result.AddError("unique-mapping",
					fmt.Sprintf("%s: tf-addr '%s' maps to multiple URNs: %s",
						stackPrefix, tfAddr, strings.Join(urns, ", ")))
			}
		}

		// Check for duplicate URN mappings
		for urn, tfAddrs := range urnToTFAddr {
			if len(tfAddrs) > 1 {
				result.AddErrorWithSuggestion("unique-mapping",
					fmt.Sprintf("%s: URN '%s' maps to multiple tf-addrs: %s",
						stackPrefix, urn, strings.Join(tfAddrs, ", ")),
					"Edit migration.json to ensure the URN mapping is unique")
			}
		}
	}
}

// checkStateConsistency verifies that resources in migration.json match the Terraform state
func checkStateConsistency(ctx context.Context, mf *MigrationFile, result *CheckResult) error {
	for i, stack := range mf.Migration.Stacks {
		stackPrefix := fmt.Sprintf("stack[%d] (%s)", i, stack.PulumiStack)

		// Skip if no tf-state is specified
		if stack.TFState == "" {
			continue
		}

		// Load the Terraform state
		state, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
			StateFilePath: stack.TFState,
		})
		if err != nil {
			return fmt.Errorf("failed to load state for %s: %w", stackPrefix, err)
		}

		// Collect all resource addresses from the state
		stateAddrs := make(map[string]bool)
		err = tofu.VisitResources(state, func(res *tfjson.StateResource) error {
			stateAddrs[res.Address] = true
			return nil
		}, nil) // Use default options (skips data sources)
		if err != nil {
			return fmt.Errorf("failed to visit resources in state for %s: %w", stackPrefix, err)
		}

		// Collect all tf-addrs from migration.json for this stack
		migrationAddrs := make(map[string]bool)
		for _, res := range stack.Resources {
			if res.TFAddr != "" {
				migrationAddrs[res.TFAddr] = true
			}
		}

		// Check for resources in state that are missing from migration.json
		for addr := range stateAddrs {
			if !migrationAddrs[addr] {
				result.AddErrorWithSuggestion("state-consistency",
					fmt.Sprintf("%s: resource '%s' exists in Terraform state but not in migration.json",
						stackPrefix, addr),
					"Add an entry for this resource to migration.json mapping it to a Pulumi resource or skipping it")
			}
		}

		// Check for resources in migration.json that don't exist in state
		for addr := range migrationAddrs {
			if !stateAddrs[addr] {
				result.AddErrorWithSuggestion("state-consistency",
					fmt.Sprintf("%s: resource '%s' exists in migration.json but not in Terraform state",
						stackPrefix, addr),
					"Remove this resource grom migration.json")
			}
		}
	}

	return nil
}
