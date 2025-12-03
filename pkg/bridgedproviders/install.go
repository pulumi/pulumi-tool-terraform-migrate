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

package pulumix

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/blang/semver"
	pkgWorkspace "github.com/pulumi/pulumi/pkg/v3/workspace"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/common/diag"
	"github.com/pulumi/pulumi/sdk/v3/go/common/workspace"
)

// InstallProviderOptions contains options for installing a provider.
type InstallProviderOptions struct {
	// Name is the provider name (e.g., "random", "aws", "azure-native")
	Name string
	// Version is the semver version string (e.g., "v4.18.4")
	Version string
	// PluginDir is the optional cache directory to install the provider to.
	PluginDir string
	// PluginDownloadURL is an optional custom server URL to download the provider from.
	PluginDownloadURL string
}

func defaultPluginDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("Unexpected error from os.UserHomeDir(): %v", err)
	}
	p := filepath.Join(home, ".pulumi-terraform-migrate", "plugins")
	return p, nil
}

// PluginDir is the optional cache directory to install the provider to.
func (opts InstallProviderOptions) EnsurePluginDir() (string, error) {
	if opts.PluginDir == "" {
		p, err := defaultPluginDir()
		if err != nil {
			return "", err
		}
		err = os.MkdirAll(p, 0700)
		if err != nil {
			return "", fmt.Errorf("Failed to ensure the plugin dir exists: %v", err)
		}
		return p, nil
	}
	return opts.PluginDir, nil
}

// InstallProviderResult contains the result of installing a provider.
type InstallProviderResult struct {
	// BinaryPath is the absolute path to the installed provider binary
	BinaryPath string
	// Version is the semver version that was installed
	Version semver.Version
	// PluginDir is the directory where the provider was installed
	PluginDir string
}

// InstallProvider automatically downloads and installs a Pulumi provider by name and version.
// It returns the path to the provider binary once installed.
//
// This function:
// 1. Creates a PluginSpec for the provider
// 2. Downloads the provider from the Pulumi plugin server (or custom URL)
// 3. Installs the provider to the plugin cache directory
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

	// Create a PluginSpec for the provider
	spec, err := workspace.NewPluginSpec(
		ctx,
		opts.Name,
		apitype.ResourcePlugin,
		&ver,
		opts.PluginDownloadURL,
		nil, // checksums
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create plugin spec: %w", err)
	}

	// Override the plugin directory if specified
	pluginDir, err := opts.EnsurePluginDir()
	if err != nil {
		return nil, err
	}
	spec.PluginDir = pluginDir

	// Install the plugin
	log := func(sev diag.Severity, msg string) {
		// Simple logging to stderr
		if sev == diag.Error || sev == diag.Warning {
			fmt.Fprintf(os.Stderr, "[%s] %s\n", sev, msg)
		}
	}

	installedVersion, err := pkgWorkspace.InstallPlugin(ctx, spec, log)
	if err != nil {
		return nil, fmt.Errorf("failed to install provider %s: %w", opts.Name, err)
	}

	// Construct the path to the installed binary
	binaryPath := getProviderBinaryPath(pluginDir, opts.Name, *installedVersion)

	return &InstallProviderResult{
		BinaryPath: binaryPath,
		Version:    *installedVersion,
		PluginDir:  pluginDir,
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
func GetInstalledProviderPath(ctx context.Context, name string, version string, pluginDir string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("name is required")
	}
	if version == "" {
		return "", fmt.Errorf("version is required")
	}
	if pluginDir == "" {
		d, err := defaultPluginDir()
		if err != nil {
			return "", err
		}
		pluginDir = d
	}

	// Parse the version
	ver, err := semver.ParseTolerant(version)
	if err != nil {
		return "", fmt.Errorf("failed to parse version %q: %w", version, err)
	}

	// Create a PluginSpec
	spec := workspace.PluginSpec{
		Name:      name,
		Kind:      apitype.ResourcePlugin,
		Version:   &ver,
		PluginDir: pluginDir,
	}

	// Use GetPluginPath to find the binary
	path, err := workspace.GetPluginPath(ctx, nil, spec, nil)
	if err != nil {
		return "", fmt.Errorf("provider %s v%s not found: %w", name, version, err)
	}

	return path, nil
}
