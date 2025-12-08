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

package bridgedproviders

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/blang/semver"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/common/workspace"
)

// InstallProviderOptions contains options for installing a provider.
type InstallProviderOptions struct {
	// Name is the provider name (e.g., "random", "aws", "azure-native")
	Name string
	// Version is the semver version string (e.g., "v4.18.4")
	Version string
	// PluginDownloadURL is an optional custom server URL to download the provider from.
	PluginDownloadURL string
}

// InstallProviderResult contains the result of installing a provider.
type InstallProviderResult struct {
	// BinaryPath is the absolute path to the installed provider binary
	BinaryPath string
	// Version is the semver version that was installed
	Version semver.Version
}

// InstallProvider automatically downloads and installs a Pulumi provider by name and version.
// It returns the path to the provider binary once installed.
//
// This function:
// 1. Creates a LocalWorkspace for plugin management
// 2. Downloads the provider from the Pulumi plugin server (or custom URL)
// 3. Installs the provider to the standard Pulumi plugin cache
// 4. Returns the path to the installed binary
//
// Example:
//
//	result, err := InstallProvider(ctx, InstallProviderOptions{
//	    Name: "random",
//	    Version: "v4.18.4",
//	})
//	if err != nil {
//	    return err
//	}
//	// result.BinaryPath is the path to pulumi-resource-random binary
func InstallProvider(ctx context.Context, opts InstallProviderOptions) (*InstallProviderResult, error) {
	if opts.Name == "" {
		return nil, fmt.Errorf("Name is required in InstallProviderOptions")
	}
	if opts.Version == "" {
		return nil, fmt.Errorf("Version is required in InstallProviderOptions")
	}

	// Parse the version
	ver, err := semver.ParseTolerant(opts.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to parse version %q: %w", opts.Version, err)
	}

	// Create a LocalWorkspace for plugin installation
	w, err := auto.NewLocalWorkspace(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create workspace: %w", err)
	}

	// Install the plugin using the automation API
	if opts.PluginDownloadURL != "" {
		err = w.InstallPluginFromServer(ctx, opts.Name, opts.Version, opts.PluginDownloadURL)
	} else {
		err = w.InstallPlugin(ctx, opts.Name, opts.Version)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to install provider %s: %w", opts.Name, err)
	}

	// Get the plugin directory
	pluginDir, err := workspace.GetPluginDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin directory: %w", err)
	}

	// Construct the path to the installed binary
	binaryPath := getProviderBinaryPath(pluginDir, opts.Name, ver)

	return &InstallProviderResult{
		BinaryPath: binaryPath,
		Version:    ver,
	}, nil
}

// getProviderBinaryPath constructs the path to a provider binary given the plugin directory,
// provider name, and version.
func getProviderBinaryPath(pluginDir, name string, version semver.Version) string {
	// Plugin directory structure: <pluginDir>/<kind>-<name>-v<version>/
	versionedDir := fmt.Sprintf("resource-%s-v%s", name, version.String())
	pluginPath := filepath.Join(pluginDir, versionedDir)

	// Binary name: pulumi-resource-<name>
	binaryName := fmt.Sprintf("pulumi-resource-%s", name)
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}

	return filepath.Join(pluginPath, binaryName)
}

// GetInstalledProviderPath returns the path to an already-installed provider binary,
// or an error if the provider is not installed.
//
// This is useful if you want to check if a provider is already installed before
// attempting to install it.
func GetInstalledProviderPath(ctx context.Context, name string, version string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("name is required")
	}
	if version == "" {
		return "", fmt.Errorf("version is required")
	}

	// Parse the version
	ver, err := semver.ParseTolerant(version)
	if err != nil {
		return "", fmt.Errorf("failed to parse version %q: %w", version, err)
	}

	// Get the plugin directory
	pluginDir, err := workspace.GetPluginDir()
	if err != nil {
		return "", fmt.Errorf("failed to get plugin directory: %w", err)
	}

	// Construct the expected binary path
	binaryPath := getProviderBinaryPath(pluginDir, name, ver)

	// Check if the binary exists
	if _, err := os.Stat(binaryPath); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("provider %s v%s not found", name, version)
		}
		return "", fmt.Errorf("failed to check provider binary: %w", err)
	}

	return binaryPath, nil
}
