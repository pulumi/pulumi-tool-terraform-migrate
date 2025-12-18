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
	// Path to the explicit terraform.tfstate file.  Only one of [ProjectDir], [StateFilePath] should be given.
	StateFilePath string

	// Path to the root directory where Terraform sources are located.
	ProjectDir string

	// If empty, extract state from a current workspace. For simple projects there is just one workspace called
	// "default". If non-empty, extract state from a given Terraform or OpenTOFU workspace instead. Requires
	// [ProjectDir] to be set.
	Workspace string
}

// LoadTerraformState loads a Terraform or OpenTOFU state. For
//
// It uses the `tofu show -json` format to convert it to the official `*tfjson.State` format.
//
// As of the time of writing, `tofu show -json` pre-supposes having access to providers and running `tofu init -json`.
// The command will init the providers if necessary.
//
// OpenTOFU has a problem reading states created by Terraform proper that rely on providers from Terraform registry.
// LoadTerraformState works around this by using a temporary workspace to convert providers to their OpenTOFU registry
// equivalents and extracts the OpenTOFU-translated state without modifying the original workspace.
//
// Common errors:
//
// - will fail if `tofu` binary is not in PATH
// - will fail if `tofu` fails to authenticate to an state backend such as the S3 state backend
//
// See also: https://github.com/pulumi/pulumi-service/issues/34864
func LoadTerraformState(ctx context.Context, opts LoadTerraformStateOptions) (finalState *tfjson.State, finalError error) {
	if opts.StateFilePath != "" {
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

	// If given an explicit StateFilePath, re-interpret it as a temp workspace.
	if opts.StateFilePath != "" {
		return loadStateFilePath(ctx, tofu, opts.ProjectDir, opts.StateFilePath)
	}

	workspace := opts.Workspace
	if workspace == "" {
		w, err := tofu.WorkspaceShow(ctx)
		if err != nil {
			return nil, err
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

// If given an explicit StateFilePath, re-interpret it as a temp workspace.
func loadStateFilePath(
	ctx context.Context,
	tofu *tfexec.Terraform,
	projectDir string,
	stateFilePath string,
) (finalState *tfjson.State, finalError error) {
	tempWorkspace, err := pickTempWorkspaceName(ctx, tofu)
	if err != nil {
		return nil, err
	}

	defer func() {
		err := tofu.WorkspaceDelete(ctx, tempWorkspace, tfexec.Force(true))
		if err != nil {
			finalError = errors.Join(finalError, err)
		}
	}()

	if err := newWorkspace(ctx, tofu, tempWorkspace, stateFilePath); err != nil {
		return nil, err
	}

	return LoadTerraformState(ctx, LoadTerraformStateOptions{
		ProjectDir: projectDir,
		Workspace:  tempWorkspace,
	})
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
		return nil, err
	}

	// Switch to the desired workspace for the duration of the func if it is not matching the currentWorkspace.
	if workspace != currentWorkspace {
		if err := tofu.WorkspaceSelect(ctx, workspace); err != nil {
			return nil, err
		}
		defer func() {
			err = tofu.WorkspaceSelect(ctx, currentWorkspace)
			if err != nil {
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
				finalError = errors.Join(finalError, err)
			}
		}()

		if err := cloneCurrentWorkspace(ctx, tofu, tempWorkspace); err != nil {
			return nil, err
		}

		// The workaround may modify .terraform.lock.hcl which will break terraform access. Save and restore
		// this file when applying the workaround..
		lockFile := filepath.Join(projectDir, ".terraform.lock.hcl")
		lockFileInfo, _ := os.Stat(lockFile)
		lockBytes, _ := os.ReadFile(lockFile)
		if len(lockBytes) > 0 {
			defer func() {
				if err := os.WriteFile(lockFile, lockBytes, lockFileInfo.Mode()); err != nil {
					finalError = errors.Join(finalError, err)
				}
			}()
		}

		if err := fixupProviderError(ctx, tofu, tempWorkspace); err != nil {
			return nil, err
		}

		return loadWorkspaceStateInner(ctx, tofu, projectDir, tempWorkspace,
			false /* workaroundRegistryError */) // avoid infinite workaround regress here
	default:
		return nil, err
	}
}

// Runs tofu init --upgrade && tofu plan && tofu apply in a given workspace.
func fixupProviderError(ctx context.Context, tofu *tfexec.Terraform, workspace string) (finalError error) {
	currentWorkspace, err := tofu.WorkspaceShow(ctx)
	if err != nil {
		return err
	}

	defer func() {
		if err := tofu.WorkspaceSelect(ctx, currentWorkspace); err != nil {
			finalError = errors.Join(finalError, err)
		}
	}()

	if err := tofu.WorkspaceSelect(ctx, workspace); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "Running tofu init -upgrade in the workspace", workspace)
	if err := tofu.Init(ctx, &tfexec.UpgradeOption{}); err != nil {
		return err
	}

	changes, err := tofu.Plan(ctx, tfexec.Refresh(false))
	if err != nil {
		return err
	}

	if changes {
		return fmt.Errorf("tofu plan detects changes, refusing to run apply")
	}

	fmt.Fprintln(os.Stderr, "Running tofu apply -refresh=false in the workspace", workspace)
	if err := tofu.Apply(ctx, tfexec.Refresh(false)); err != nil {
		return err
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
		return err
	}

	defer func() {
		if err := tofu.WorkspaceSelect(ctx, currentWorkspace); err != nil {
			finalError = errors.Join(finalError, err)
		}
	}()

	if err := tofu.WorkspaceNew(ctx, newWorkspaceName); err != nil {
		return err
	}

	if err := tofu.WorkspaceSelect(ctx, newWorkspaceName); err != nil {
		return err
	}

	if err := tofu.StatePush(ctx, stateFilePath); err != nil {
		return err
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
		return "", err
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
