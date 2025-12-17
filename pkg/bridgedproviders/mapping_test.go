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
	"testing"
)

// This is an example test showing how GetMappingFromBinary would be used.
// To run this test, you would need a real Pulumi provider binary that implements GetMapping.
func TestGetMappingFromBinary_Example(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Install a small provider like random
	installRandomResult, err := InstallProvider(ctx, InstallProviderOptions{
		Name:    "random",
		Version: "v4.16.7",
	})
	if err != nil {
		t.Fatalf("InstallProvider failed: %v", err)
	}

	// Example: Get terraform mapping from pulumi-aws provider
	result, err := GetMappingFromBinary(ctx, installRandomResult.BinaryPath, GetMappingOptions{
		Key: "terraform",
	})
	if err != nil {
		t.Fatalf("GetMappingFromBinary failed: %v", err)
	}

	if result.Provider != "random" {
		t.Errorf("Expected provider 'random', got '%s'", result.Provider)
	}

	if len(result.Data) == 0 {
		t.Errorf("Expected non-empty mapping data")
	}

	// The mapping data would typically be JSON that can be parsed
	t.Logf("Retrieved mapping for provider '%s', data size: %d bytes", result.Provider, len(result.Data))
}

func TestGetMappingFromBinary_RequiresBinaryPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	_, err := GetMappingFromBinary(ctx, "", GetMappingOptions{
		Key: "terraform",
	})
	if err == nil {
		t.Fatal("Expected error when binaryPath is empty")
	}
}

func TestGetMappingFromBinary_RequiresKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	_, err := GetMappingFromBinary(ctx, "/path/to/binary", GetMappingOptions{
		Provider: "aws",
	})
	if err == nil {
		t.Fatal("Expected error when Key is empty")
	}
}
