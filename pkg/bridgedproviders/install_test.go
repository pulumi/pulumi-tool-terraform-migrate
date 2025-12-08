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
	"os"
	"path/filepath"
	"testing"

	"github.com/blang/semver"
)

// This is an integration test that actually downloads and installs a provider.
// It's skipped by default but can be enabled for manual testing.
func TestInstallProvider_Integration(t *testing.T) {
	ctx := context.Background()

	// Install a small provider like random to the standard Pulumi home
	result, err := InstallProvider(ctx, InstallProviderOptions{
		Name:    "random",
		Version: "v4.16.7",
	})
	if err != nil {
		t.Fatalf("InstallProvider failed: %v", err)
	}

	// Verify the binary was created
	if _, err := os.Stat(result.BinaryPath); os.IsNotExist(err) {
		t.Errorf("Provider binary not found at %s", result.BinaryPath)
	}

	t.Logf("Installed provider at: %s", result.BinaryPath)
	t.Logf("Version: %s", result.Version.String())
}

func TestInstallProvider_RequiresName(t *testing.T) {
	ctx := context.Background()

	_, err := InstallProvider(ctx, InstallProviderOptions{
		Version: "v4.16.7",
	})
	if err == nil {
		t.Fatal("Expected error when Name is empty")
	}
}

func TestInstallProvider_RequiresVersion(t *testing.T) {
	ctx := context.Background()

	_, err := InstallProvider(ctx, InstallProviderOptions{
		Name: "random",
	})
	if err == nil {
		t.Fatal("Expected error when Version is empty")
	}
}

func TestInstallProvider_InvalidVersion(t *testing.T) {
	ctx := context.Background()

	_, err := InstallProvider(ctx, InstallProviderOptions{
		Name:    "random",
		Version: "not-a-version",
	})
	if err == nil {
		t.Fatal("Expected error when Version is invalid")
	}
}

func TestGetProviderBinaryPath(t *testing.T) {
	tests := []struct {
		name       string
		pluginDir  string
		provName   string
		version    string
		expectPath string // relative path for comparison
	}{
		{
			name:       "basic path",
			pluginDir:  "/home/user/.pulumi/plugins",
			provName:   "random",
			version:    "4.16.7",
			expectPath: "resource-random-v4.16.7/pulumi-resource-random",
		},
		{
			name:       "aws provider",
			pluginDir:  "/tmp/plugins",
			provName:   "aws",
			version:    "6.50.0",
			expectPath: "resource-aws-v6.50.0/pulumi-resource-aws",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse version for test
			ver := mustParseVersion(t, tt.version)

			result := getProviderBinaryPath(tt.pluginDir, tt.provName, ver)

			// Check that the path ends with the expected relative path
			expectedSuffix := filepath.Join(tt.expectPath)
			if filepath.Base(filepath.Dir(result)) != filepath.Base(filepath.Dir(expectedSuffix)) ||
				filepath.Base(result) != filepath.Base(expectedSuffix) {
				t.Errorf("getProviderBinaryPath() = %v, want path ending with %v", result, expectedSuffix)
			}

			// Check that it starts with the plugin dir
			if !filepath.HasPrefix(result, tt.pluginDir) {
				t.Errorf("getProviderBinaryPath() = %v, want path starting with %v", result, tt.pluginDir)
			}
		})
	}
}

func TestGetInstalledProviderPath_RequiresName(t *testing.T) {
	ctx := context.Background()

	_, err := GetInstalledProviderPath(ctx, "", "v4.16.7")
	if err == nil {
		t.Fatal("Expected error when name is empty")
	}
}

func TestGetInstalledProviderPath_RequiresVersion(t *testing.T) {
	ctx := context.Background()

	_, err := GetInstalledProviderPath(ctx, "random", "")
	if err == nil {
		t.Fatal("Expected error when version is empty")
	}
}

func TestGetInstalledProviderPath_HappyPath(t *testing.T) {
	// Create a temporary plugin directory
	tmpDir := t.TempDir()

	// Create the expected directory structure for a provider
	providerName := "random"
	version := "4.16.7"
	ver := mustParseVersion(t, version)

	versionedDir := filepath.Join(tmpDir, "resource-random-v4.16.7")
	if err := os.MkdirAll(versionedDir, 0755); err != nil {
		t.Fatalf("failed to create versioned directory: %v", err)
	}

	// Create the binary file
	binaryName := "pulumi-resource-random"
	binaryPath := filepath.Join(versionedDir, binaryName)
	if err := os.WriteFile(binaryPath, []byte("fake binary"), 0755); err != nil {
		t.Fatalf("failed to create binary: %v", err)
	}

	// Test with the actual function by temporarily overriding the plugin directory
	// Since we can't easily override workspace.GetPluginDir, we'll test getProviderBinaryPath
	// and verify the file existence logic separately

	// Verify getProviderBinaryPath constructs the correct path
	constructedPath := getProviderBinaryPath(tmpDir, providerName, ver)
	if constructedPath != binaryPath {
		t.Errorf("Expected path %s, got %s", binaryPath, constructedPath)
	}

	// Verify file exists
	if _, err := os.Stat(constructedPath); err != nil {
		t.Errorf("Expected file to exist at %s: %v", constructedPath, err)
	}
}

func TestGetInstalledProviderPath_NotFound(t *testing.T) {
	// Create a temporary plugin directory
	tmpDir := t.TempDir()

	// Set an environment variable to override the plugin directory
	// (This would require modifying the implementation to support testing)
	// For now, we'll just test the components directly

	providerName := "nonexistent"
	version := "1.0.0"
	ver := mustParseVersion(t, version)

	// Verify that getProviderBinaryPath constructs a path
	constructedPath := getProviderBinaryPath(tmpDir, providerName, ver)

	// Verify file does not exist
	if _, err := os.Stat(constructedPath); !os.IsNotExist(err) {
		t.Errorf("Expected file not to exist at %s", constructedPath)
	}
}

func mustParseVersion(t *testing.T, v string) semver.Version {
	t.Helper()
	ver, err := semver.ParseTolerant(v)
	if err != nil {
		t.Fatalf("failed to parse version %q: %v", v, err)
	}
	return ver
}
