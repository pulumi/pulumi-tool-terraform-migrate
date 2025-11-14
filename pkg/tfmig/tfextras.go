package tfmig

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tfjson "github.com/hashicorp/terraform-json"
)

// LoadTerraformState loads a Terraform state file from either JSON format (.json)
// or raw binary format (.tfstate). It returns the parsed state.
// For binary format, it uses `terraform show -json` to convert to JSON first.
func LoadTerraformState(path string) (*tfjson.State, error) {
	// Check file extension to determine format
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".json":
		// File is already in JSON format, read directly
		return ReadTerraformStateJSON(path)

	case ".tfstate":
		// File is in binary format, need to convert using terraform show
		// Get the directory containing the state file
		stateDir := filepath.Dir(path)

		// Run terraform show -json on the state file
		cmd := exec.Command("terraform", "show", "-json", path)
		cmd.Dir = stateDir
		output, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("failed to convert binary state file using terraform show: %w", err)
		}

		// Parse the JSON output
		var state tfjson.State
		if err := json.Unmarshal(output, &state); err != nil {
			return nil, fmt.Errorf("failed to parse state JSON from terraform show: %w", err)
		}

		return &state, nil

	default:
		return nil, fmt.Errorf("unsupported state file format: %s (expected .json or .tfstate)", ext)
	}
}

// ReadTerraformStateJSON reads a Terraform state file in JSON format
// (produced by `terraform show -json`) and returns the parsed state.
// Deprecated: Use LoadTerraformState instead, which handles both JSON and binary formats.
func ReadTerraformStateJSON(path string) (*tfjson.State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state tfjson.State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state JSON: %w", err)
	}

	return &state, nil
}

// VisitResources walks through all resources in a Terraform state,
// calling the visitor function for each resource found.
// It recursively traverses the root module and all child modules.
func VisitResources(state *tfjson.State, visitor func(*tfjson.StateResource) error) error {
	if state == nil || state.Values == nil || state.Values.RootModule == nil {
		return nil
	}
	return visitModule(state.Values.RootModule, visitor)
}

// AllResources returns all resources from a Terraform state
func AllResources(state *tfjson.State) ([]*tfjson.StateResource, error) {
	var results []*tfjson.StateResource
	VisitResources(state, func(sr *tfjson.StateResource) error {
		results = append(results, sr)
		return nil
	})
	return results, nil
}

// visitModule recursively visits all resources in a module and its child modules.
func visitModule(module *tfjson.StateModule, visitor func(*tfjson.StateResource) error) error {
	if module == nil {
		return nil
	}

	// Visit resources in this module
	for _, res := range module.Resources {
		if res.Mode == tfjson.DataResourceMode {
			continue
		}
		if err := visitor(res); err != nil {
			return err
		}
	}

	// Recursively visit child modules
	for _, child := range module.ChildModules {
		if err := visitModule(child, visitor); err != nil {
			return err
		}
	}

	return nil
}
