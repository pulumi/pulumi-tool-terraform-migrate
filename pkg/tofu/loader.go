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

package tofu

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hashicorp/terraform-exec/tfexec"
	tfjson "github.com/hashicorp/terraform-json"
)

// LoadTerraformState loads a Terraform state file from either JSON format (.json)
// or raw binary format (.tfstate). It returns the parsed state.
// For binary format, it uses `tofu show -json` to convert to JSON first.
func LoadTerraformState(path string) (*tfjson.State, error) {
	// Check file extension to determine format
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".json":
		// File is already in JSON format, read directly
		return ReadTerraformStateJSON(path)

	case ".tfstate":
		// File is in binary format, need to convert using tofu show
		return loadBinaryStateWithTofu(path)

	default:
		return nil, fmt.Errorf("unsupported state file format: %s (expected .json or .tfstate)", ext)
	}
}

// loadBinaryStateWithTofu uses terraform-exec to run `tofu show -json` on a binary state file
func loadBinaryStateWithTofu(statePath string) (*tfjson.State, error) {
	// Locate the tofu binary in PATH
	tofuPath, err := exec.LookPath("tofu")
	if err != nil {
		return nil, fmt.Errorf("tofu binary not found in PATH: %w", err)
	}

	// Get the directory containing the state file
	stateDir := filepath.Dir(statePath)

	// Create a terraform-exec instance with the tofu binary
	tf, err := tfexec.NewTerraform(stateDir, tofuPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create terraform-exec instance: %w", err)
	}

	// Run tofu show -json on the state file
	ctx := context.Background()
	state, err := tf.ShowStateFile(ctx, statePath)
	if err != nil {
		return nil, fmt.Errorf("failed to convert binary state file using tofu show: %w", err)
	}

	return state, nil
}

// ReadTerraformStateJSON reads and parses a Terraform state file that's already in JSON format
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
