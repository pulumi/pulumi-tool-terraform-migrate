package tfmig

import (
	"fmt"
	"os"
	"strings"

	tfjson "github.com/hashicorp/terraform-json"
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
func CheckMigrationIntegrity(migrationFile *MigrationFile) (*CheckResult, error) {
	result := &CheckResult{}

	// Check 1: Verify files exist
	checkFilesExist(migrationFile, result)

	// Check 2: Verify unique tf-addr to URN mapping (both directions)
	checkUniqueMapping(migrationFile, result)

	// Check 3: Verify resources match Terraform state
	if err := checkStateConsistency(migrationFile, result); err != nil {
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

		// Check import-stub-file
		if stack.ImportStubFile != "" {
			if _, err := os.Stat(stack.ImportStubFile); err != nil {
				result.AddError("file-existence", fmt.Sprintf("%s: import-stub-file does not exist: %s", stackPrefix, stack.ImportStubFile))
			}
		}

		// Check import-resolved-file
		if stack.ImportResolvedFile != "" {
			if _, err := os.Stat(stack.ImportResolvedFile); err != nil {
				result.AddError("file-existence", fmt.Sprintf("%s: import-resolved-file does not exist: %s", stackPrefix, stack.ImportResolvedFile))
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

		for _, res := range stack.Resources {
			// Skip resources that should be skipped or have no URN
			if res.Migrate != MigrateModeEmpty && res.Migrate != "" {
				continue
			}
			if res.URN == "" {
				continue
			}
			if res.TFAddr == "" {
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
				suggestions := make([]string, 0, len(tfAddrs)-1)
				for j := 1; j < len(tfAddrs); j++ {
					suggestions = append(suggestions, fmt.Sprintf("pulumi-terraform-migrate set-urn --addr '%s' --urn '<different-urn>' --stack '%s'", tfAddrs[j], stack.PulumiStack))
				}
				result.AddErrorWithSuggestion("unique-mapping",
					fmt.Sprintf("%s: URN '%s' maps to multiple tf-addrs: %s",
						stackPrefix, urn, strings.Join(tfAddrs, ", ")),
					strings.Join(suggestions, " OR "))
			}
		}
	}
}

// checkStateConsistency verifies that resources in migration.json match the Terraform state
func checkStateConsistency(mf *MigrationFile, result *CheckResult) error {
	for i, stack := range mf.Migration.Stacks {
		stackPrefix := fmt.Sprintf("stack[%d] (%s)", i, stack.PulumiStack)

		// Skip if no tf-state is specified
		if stack.TFState == "" {
			continue
		}

		// Load the Terraform state
		state, err := LoadTerraformState(stack.TFState)
		if err != nil {
			return fmt.Errorf("failed to load state for %s: %w", stackPrefix, err)
		}

		// Collect all resource addresses from the state
		stateAddrs := make(map[string]bool)
		err = VisitResources(state, func(res *tfjson.StateResource) error {
			// Only track managed resources, not data sources
			if res.Mode == tfjson.DataResourceMode {
				return nil
			}
			stateAddrs[res.Address] = true
			return nil
		})
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
					fmt.Sprintf("pulumi-terraform-migrate skip --addr '%s' --stack '%s'", addr, stack.PulumiStack))
			}
		}

		// Check for resources in migration.json that don't exist in state
		for addr := range migrationAddrs {
			if !stateAddrs[addr] {
				result.AddErrorWithSuggestion("state-consistency",
					fmt.Sprintf("%s: resource '%s' exists in migration.json but not in Terraform state",
						stackPrefix, addr),
					fmt.Sprintf("pulumi-terraform-migrate untrack '%s'", addr))
			}
		}
	}

	return nil
}
