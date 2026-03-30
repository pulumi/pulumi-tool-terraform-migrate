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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hashicorp/terraform-exec/tfexec"
	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/contract"
)

// See [LoadTerraformState].
type LoadTerraformStateOptions struct {
	// Path to the explicit `terraform.tfstate` file.
	//
	// To facilitate testing, if this file path ends with .json it is simply read directly and is assumed to be in
	// the format emitted by `tofu show -json`.
	//
	// Only one of [ProjectDir], [StateFilePath] should be given.
	StateFilePath string

	// Path to the root directory where Terraform sources are located.
	ProjectDir string

	// If non-empty, extract state from a given Terraform or OpenTofu workspace.
	//
	// If empty, extract state from the current workspace, typically "default".
	//
	// Requires [ProjectDir] to be set.
	Workspace string
}

// TofuVersionOutput represents the output of `tofu version -json`
type TofuVersionOutput struct {
	TerraformVersion string `json:"terraform_version"`
	Platform         string `json:"platform"`

	// A map of provider identifiers to their resolved versions, e.g.:
	//
	//	{"registry.terraform.io/hashicorp/random": "3.7.2"}
	ProviderSelections map[string]string `json:"provider_selections"`
}

// LoadTerraformState loads a Terraform or OpenTofu state.
//
// Requires `tofu` in path and executes these commands:
//
//	tofu init
//	tofu show -json
//	tofu workspace show/select
//	tofu state pull
//
// OpenTofu sometimes has a problem reading states created by Terraform proper that rely on providers from the
// Terraform registry. LoadTerraformState works around this by rewriting registry.terraform.io provider references
// to registry.opentofu.org in the state JSON, then parsing the rewritten state with `tofu show -json`.
//
// Common errors:
//
// - will fail if `tofu` binary is not in PATH
// - will fail if `tofu` fails to authenticate to a state backend such as the S3 state backend
//
// See also: https://github.com/pulumi/pulumi-service/issues/34864
func LoadTerraformState(ctx context.Context, opts LoadTerraformStateOptions) (finalState *tfjson.State, finalError error) {
	if opts.StateFilePath != "" {
		// Direct reading JSON case to facilitate testing.
		if filepath.Ext(opts.StateFilePath) == ".json" {
			bytes, err := os.ReadFile(opts.StateFilePath)
			if err != nil {
				return nil, err
			}
			var st tfjson.State
			if err := json.Unmarshal(bytes, &st); err != nil {
				return nil, err
			}
			return &st, nil
		}

		contract.Assertf(opts.Workspace == "", "Workspace is not compatible with StateFilePath")
		if opts.ProjectDir == "" {
			opts.ProjectDir = filepath.Dir(opts.StateFilePath)
		}
	} else {
		contract.Assertf(opts.ProjectDir != "", "ProjectDir or StateFilePath is required")
	}

	tofu, err := tofuNew(opts.ProjectDir)
	if err != nil {
		return nil, err
	}

	// The code may modify `.terraform.lock.hcl` or create a `.terraform.lock.hcl` by OpenTofu which will break
	// `terraform plan` subsequently. Save and restore this file when applying the workaround.
	lockFile := filepath.Join(opts.ProjectDir, ".terraform.lock.hcl")
	lockFileInfo, _ := os.Stat(lockFile)
	lockBytes, _ := os.ReadFile(lockFile)
	defer func() {
		if len(lockBytes) > 0 {
			if err := os.WriteFile(lockFile, lockBytes, lockFileInfo.Mode()); err != nil {
				finalError = errors.Join(finalError, err)
			}
		} else if err := os.RemoveAll(lockFile); err != nil {
			finalError = errors.Join(finalError, err)
		}
	}()

	// Similarly stash away and restore `.terraform` directory to avoid OpenTofu breaking Terraform.
	dotTerraform := filepath.Join(opts.ProjectDir, ".terraform")
	dotTerraformBak := generateUniqueBackupFileName(opts.ProjectDir, ".terraform")
	stat, err := os.Stat(dotTerraform)
	dotTerraformExists := err == nil && stat.IsDir()

	if dotTerraformExists {
		if err := os.Rename(dotTerraform, dotTerraformBak); err != nil {
			return nil, fmt.Errorf("Error backing up .terraform folder; %w", err)
		}
	}

	defer func() {
		err := os.RemoveAll(dotTerraform)
		contract.IgnoreError(err)

		if dotTerraformExists {
			if err := os.Rename(dotTerraformBak, dotTerraform); err != nil {
				err = fmt.Errorf("Error restoring .terraform backup; %w", err)
				finalError = errors.Join(finalError, err)
			}
		}
	}()

	// Almost every tofu operation assumes init and will fail it not initialized; init early. Running tofu init is
	// a cached operation that is cheaper the second time around it reuses the lock file and provider downloads
	// under .terraform.
	if err := tofu.Init(ctx); err != nil {
		return nil, fmt.Errorf("tofu init failed: %w", err)
	}

	// If given an explicit StateFilePath, try ShowStateFile first; fall back to provider rewrite
	// if OpenTofu cannot resolve Terraform registry provider references.
	if opts.StateFilePath != "" {
		absStateFile, err := filepath.Abs(opts.StateFilePath)
		if err != nil {
			return nil, fmt.Errorf("resolving state file path: %w", err)
		}
		state, err := tofu.ShowStateFile(ctx, absStateFile)
		if err == nil {
			return state, nil
		}
		if strings.Contains(err.Error(), "Failed to load plugin schemas") &&
			strings.Contains(err.Error(), "while loading schemas for plugin components") {
			fmt.Fprintln(os.Stderr, "Error reading state file with OpenTofu. Rewriting provider references.")
			return loadStateFileWithRewrite(ctx, tofu, absStateFile)
		}
		return nil, fmt.Errorf("tofu show on state file failed: %w", err)
	}

	workspace := opts.Workspace
	if workspace == "" {
		w, err := tofu.WorkspaceShow(ctx)
		if err != nil {
			return nil, fmt.Errorf("tofu workspace show failed: %w", err)
		}
		workspace = w
	}

	return loadWorkspaceState(ctx, tofu, workspace)
}

func tofuNew(projectDir string) (*tfexec.Terraform, error) {
	// Locate the tofu binary in PATH
	tofuPath, err := exec.LookPath("tofu")
	if err != nil {
		return nil, fmt.Errorf("tofu binary not found in PATH: %w", err)
	}

	// Create a terraform-exec instance with the tofu binary
	tofu, err := tfexec.NewTerraform(projectDir, tofuPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create terraform-exec instance: %w", err)
	}

	return tofu, nil
}

// GetProviderVersions extracts resolved provider versions from a Terraform/OpenTofu project directory. This should be
// called after tofu init has been run on the project, otherwise the versions may still be unresolved.
func GetProviderVersions(ctx context.Context, projectDir string) (TofuVersionOutput, error) {
	tofu, err := tofuNew(projectDir)
	if err != nil {
		return TofuVersionOutput{}, err
	}
	return getProviderVersions(ctx, tofu)
}

func getProviderVersions(ctx context.Context, tofu *tfexec.Terraform) (TofuVersionOutput, error) {
	// Run tofu version -json
	cmd := exec.CommandContext(ctx, tofu.ExecPath(), "version", "-json")
	cmd.Dir = tofu.WorkingDir()

	var versionOutput TofuVersionOutput

	output, err := cmd.Output()
	if err != nil {
		return versionOutput, fmt.Errorf("failed to run tofu version -json: %w", err)
	}

	if err := json.Unmarshal(output, &versionOutput); err != nil {
		return versionOutput, fmt.Errorf("failed to parse tofu version output: %w", err)
	}

	return versionOutput, nil
}

func loadWorkspaceState(
	ctx context.Context,
	tofu *tfexec.Terraform,
	workspace string,
) (finalState *tfjson.State, finalError error) {
	currentWorkspace, err := tofu.WorkspaceShow(ctx)
	if err != nil {
		return nil, fmt.Errorf("tofu workspace show failed: %w", err)
	}

	// Switch to the desired workspace for the duration of the func if it is not matching the currentWorkspace.
	if workspace != currentWorkspace {
		if err := tofu.WorkspaceSelect(ctx, workspace); err != nil {
			return nil, fmt.Errorf("tofu workspace select failed: %w", err)
		}
		defer func() {
			err = tofu.WorkspaceSelect(ctx, currentWorkspace)
			if err != nil {
				err = fmt.Errorf("tofu workspace select failed: %w", err)
				finalError = errors.Join(finalError, err)
			}
		}()
	}

	state, err := tofu.Show(ctx)
	switch {
	case err == nil:
		return state, nil

	// Working around this error: https://github.com/pulumi/pulumi-service/issues/34864
	case strings.Contains(err.Error(), "Failed to load plugin schemas") &&
		strings.Contains(err.Error(), "while loading schemas for plugin components"):

		fmt.Fprintln(os.Stderr, "Error reading Terraform-generated state with OpenTofu. Rewriting provider references.")
		return loadStateWithRewrite(ctx, tofu)

	default:
		return nil, fmt.Errorf("tofu show failed: %w", err)
	}
}

func generateUniqueBackupFileName(dir, prefix string) string {
	i := 0
	for {
		candidate := filepath.Join(dir, fmt.Sprintf("%s.bak%d", prefix, i))
		if i == 0 {
			candidate = filepath.Join(dir, fmt.Sprintf("%s.bak", prefix))
		}
		if !fileOrFolderExists(candidate) {
			return candidate
		}
		i++

		contract.Assertf(i < 1000,
			"Failed to generate a unique %s.bak1, .bak2, .bak3 filename in %q after i=1000 iterations",
			prefix, dir)
	}
}

func fileOrFolderExists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, fs.ErrNotExist)
}

// loadStateWithRewrite pulls state from the backend, rewrites registry.terraform.io provider references
// to registry.opentofu.org, and parses via `tofu show -json`. This is used as a fallback when `tofu show`
// fails due to Terraform registry provider references that OpenTofu cannot resolve.
func loadStateWithRewrite(ctx context.Context, tofu *tfexec.Terraform) (*tfjson.State, error) {
	stateData, err := tofu.StatePull(ctx)
	if err != nil {
		return nil, fmt.Errorf("tofu state pull failed: %w", err)
	}

	return parseStateWithProviderRewrite(ctx, tofu, []byte(stateData))
}

// loadStateFileWithRewrite reads a local state file, rewrites registry.terraform.io provider references
// to registry.opentofu.org, and parses via `tofu show -json`. This is used when loading state from an
// explicit StateFilePath.
func loadStateFileWithRewrite(ctx context.Context, tofu *tfexec.Terraform, stateFilePath string) (*tfjson.State, error) {
	stateData, err := os.ReadFile(stateFilePath)
	if err != nil {
		return nil, fmt.Errorf("reading state file failed: %w", err)
	}

	return parseStateWithProviderRewrite(ctx, tofu, stateData)
}

// parseStateWithProviderRewrite rewrites registry.terraform.io → registry.opentofu.org in state JSON,
// writes to a temp file, and uses `tofu show -json` to parse it.
func parseStateWithProviderRewrite(ctx context.Context, tofu *tfexec.Terraform, stateData []byte) (*tfjson.State, error) {
	rewritten := strings.ReplaceAll(string(stateData), "registry.terraform.io/", "registry.opentofu.org/")

	tempFile, err := os.CreateTemp("", "temp-tofu-rewritten-state*.tfstate")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tempFile.Name())

	if _, err := tempFile.WriteString(rewritten); err != nil {
		return nil, err
	}
	if err := tempFile.Close(); err != nil {
		return nil, err
	}

	state, err := tofu.ShowStateFile(ctx, tempFile.Name())
	if err != nil {
		return nil, fmt.Errorf("tofu show on rewritten state file failed: %w", err)
	}

	return state, nil
}
