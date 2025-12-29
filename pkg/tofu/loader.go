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
	"io"
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

// LoadTerraformState loads a Terraform or OpenTofu state.
//
// Requires `tofu` in path and executes these commands:
//
//		tofu init
//		tofu refresh
//		tofu show -json
//	     tofu workspace *
//	     tofu state {push,pull}
//
// OpenTofu sometimes has a problem reading states created by Terraform proper that rely on providers from the
// Terraform registry. LoadTerraformState works around this by using a temporary workspace to convert providers to
// their OpenTofu registry equivalents and extracts the OpenTofu-translated state without modifying the original
// workspace. This workaround calls `tofu refresh` to compensate.
//
// Common errors:
//
// - will fail if `tofu` binary is not in PATH
// - will fail if `tofu` fails to authenticate to an state backend such as the S3 state backend
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
	dotTerraformBak := filepath.Join(opts.ProjectDir, ".terraform.bak")
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

	// Almost every tofu operation assumes init and will fail; init early.
	if err := tofu.Init(ctx); err != nil {
		return nil, fmt.Errorf("tofu init failed: %w", err)
	}

	// If given an explicit StateFilePath, re-interpret it as a temp workspace.
	if opts.StateFilePath != "" {
		tempWorkspace, err := pickTempWorkspaceName(ctx, tofu)
		if err != nil {
			return nil, err
		}

		defer func() {
			err := tofu.WorkspaceDelete(ctx, tempWorkspace, tfexec.Force(true))
			if err != nil {
				err = fmt.Errorf("deleting tofu workspace failed: %w", err)
				finalError = errors.Join(finalError, err)
			}
		}()

		if err := newWorkspace(ctx, tofu, tempWorkspace, opts.StateFilePath); err != nil {
			return nil, fmt.Errorf("temp workspace construction failed: %w", err)
		}

		opts = LoadTerraformStateOptions{
			ProjectDir: opts.ProjectDir,
			Workspace:  tempWorkspace,
		}
	}

	workspace := opts.Workspace
	if workspace == "" {
		w, err := tofu.WorkspaceShow(ctx)
		if err != nil {
			return nil, fmt.Errorf("tofu workspace show failed: %w", err)
		}
		workspace = w
	}

	return loadWorkspaceState(ctx, tofu, opts.ProjectDir, workspace)
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

func loadWorkspaceState(
	ctx context.Context,
	tofu *tfexec.Terraform,
	projectDir string,
	workspace string,
) (finalState *tfjson.State, finalError error) {
	return loadWorkspaceStateInner(ctx, tofu, projectDir, workspace, true /* workaroundRegistryError */)
}

func loadWorkspaceStateInner(
	ctx context.Context,
	tofu *tfexec.Terraform,
	projectDir string,
	workspace string,
	workaroundRegistryError bool,
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
		strings.Contains(err.Error(), "while loading schemas for plugin components") &&
		workaroundRegistryError:

		fmt.Fprintln(os.Stderr, "Error reading Terraform-generated state with OpenTofu. Computing OpenTofu state.")

		tempWorkspace, err := pickTempWorkspaceName(ctx, tofu)
		if err != nil {
			return nil, err
		}

		fmt.Fprintln(os.Stderr, "Creating a temporary workspace", tempWorkspace)

		defer func() {
			fmt.Fprintln(os.Stderr, "Cleaning up the temporary workspace", tempWorkspace)

			err := tofu.WorkspaceDelete(ctx, tempWorkspace, tfexec.Force(true))
			if err != nil {
				err = fmt.Errorf("tofu workspace delete failed: %w", err)
				finalError = errors.Join(finalError, err)
			}
		}()

		if err := cloneCurrentWorkspace(ctx, tofu, tempWorkspace); err != nil {
			return nil, err
		}

		if err := fixupProviderError(ctx, tofu, tempWorkspace); err != nil {
			return nil, err
		}

		return loadWorkspaceStateInner(ctx, tofu, projectDir, tempWorkspace,
			false /* workaroundRegistryError */) // avoid infinite workaround regress here
	default:
		return nil, fmt.Errorf("tofu show failed: %w", err)
	}
}

// Runs tofu refresh in a given workspace.
func fixupProviderError(ctx context.Context, tofu *tfexec.Terraform, workspace string) (finalError error) {
	currentWorkspace, err := tofu.WorkspaceShow(ctx)
	if err != nil {
		return fmt.Errorf("tofu workspace show failed: %w", err)
	}

	defer func() {
		if err := tofu.WorkspaceSelect(ctx, currentWorkspace); err != nil {
			err = fmt.Errorf("tofu workspace select failed: %w", err)
			finalError = errors.Join(finalError, err)
		}
	}()

	if err := tofu.WorkspaceSelect(ctx, workspace); err != nil {
		err = fmt.Errorf("tofu workspace select failed: %w", err)
		return err
	}

	fmt.Fprintln(os.Stderr, "Running tofu refresh in the workspace", workspace)
	if err := tofu.Refresh(ctx); err != nil {
		return fmt.Errorf("tofu refresh failed: %w", err)
	}

	return nil
}

// Creates destWorkspace with the given state file. Does not modify the currently selected workspace.
func newWorkspace(
	ctx context.Context,
	tofu *tfexec.Terraform,
	newWorkspaceName string,
	stateFilePath string,
) (finalError error) {
	currentWorkspace, err := tofu.WorkspaceShow(ctx)
	if err != nil {
		err = fmt.Errorf("tofu workspace show failed: %w", err)
		return err
	}

	defer func() {
		if err := tofu.WorkspaceSelect(ctx, currentWorkspace); err != nil {
			err = fmt.Errorf("tofu workspace select failed: %w", err)
			finalError = errors.Join(finalError, err)
		}
	}()

	if err := tofu.WorkspaceNew(ctx, newWorkspaceName); err != nil {
		err = fmt.Errorf("tofu workspace new failed: %w", err)
		return err
	}

	if err := tofu.WorkspaceSelect(ctx, newWorkspaceName); err != nil {
		err = fmt.Errorf("tofu workspace select failed: %w", err)
		return err
	}

	if err := tofuStatePush(ctx, tofu, stateFilePath); err != nil {
		return err
	}

	return nil
}

// Wraps tofu state push to take care of relative paths.
func tofuStatePush(ctx context.Context, tofu *tfexec.Terraform, stateFilePath string) error {
	absPath, err := filepath.Abs(stateFilePath)
	if err != nil {
		return err
	}

	if err := tofu.StatePush(ctx, absPath); err != nil {
		return fmt.Errorf("tofu state push failed: %w", err)
	}

	return nil
}

// A variation of cloneWorkspace that extracts the state from the current workspace first.
func cloneCurrentWorkspace(
	ctx context.Context,
	tofu *tfexec.Terraform,
	destWorkspace string,
) (finalError error) {
	stateData, err := tofu.StatePull(ctx)
	if err != nil {
		return err
	}

	tempFile, err := os.CreateTemp("", "temp-tofu-state*.json")
	if err != nil {
		return err
	}

	if _, err = io.Copy(tempFile, strings.NewReader(stateData)); err != nil {
		return err
	}

	defer func() {
		err := os.Remove(tempFile.Name())
		if err != nil {
			finalError = errors.Join(finalError, err)
		}
	}()

	if err := newWorkspace(ctx, tofu, destWorkspace, tempFile.Name()); err != nil {
		return err
	}

	return nil
}

// Picks a new name that does not conflict with any existing workspaces.
func pickTempWorkspaceName(ctx context.Context, tofu *tfexec.Terraform) (string, error) {
	allWorkspaces, currentWorkspace, err := tofu.WorkspaceList(ctx)
	if err != nil {
		return "", fmt.Errorf("failed listing tofu workspaces: %w", err)
	}

	allWorkspacesSet := make(map[string]struct{})
	for _, w := range allWorkspaces {
		allWorkspacesSet[w] = struct{}{}
	}

	i := 0
	for {
		var candidate string
		if i == 0 {
			candidate = fmt.Sprintf("%s-temp", currentWorkspace)
		} else {
			candidate = fmt.Sprintf("%s-temp%d", currentWorkspace, i)
		}

		_, taken := allWorkspacesSet[candidate]
		if !taken {
			return candidate, nil
		}

		if i > 1000 {
			return "", fmt.Errorf("failed to allocate a temp workspace name")
		}

		i++
	}
}
