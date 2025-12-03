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

package upsert

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	pulumix "github.com/pulumi/pulumi-terraform-migrate/pkg/bridgedproviders"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
)

func TestUpsertS3Bucket(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Setenv("PULUMI_CONFIG_PASSPHRASE", "123456")

	ctx := context.Background()

	// Install AWS provider to get the binary path
	t.Log("Installing AWS provider...")
	providerResult, err := pulumix.InstallProvider(ctx, pulumix.InstallProviderOptions{
		Name:    "aws",
		Version: "v6.64.0", // Using a recent stable version
	})
	if err != nil {
		t.Fatalf("failed to install AWS provider: %v", err)
	}
	t.Logf("AWS provider installed at: %s", providerResult.BinaryPath)

	// Set up test directory
	testDir := filepath.Join("testdata", "s3bucket")
	absTestDir, err := filepath.Abs(testDir)
	if err != nil {
		t.Fatalf("failed to get absolute path: %v", err)
	}

	// Clean up any existing stack
	stackName := "test-upsert"
	cleanupStack(t, absTestDir, stackName)

	// Initialize a new stack
	initStack(t, absTestDir, stackName)
	defer cleanupStack(t, absTestDir, stackName)

	// Define the resource to upsert
	bucketName := "test-bucket-upsert-demo"
	bucketArn := "arn:aws:s3:::test-bucket-upsert-demo"
	urn := resource.URN("urn:pulumi:test-upsert::s3bucket-test::aws:s3/bucket:Bucket::test-bucket")

	resourceSpec := ResourceSpec{
		URN:  urn,
		ID:   resource.ID(bucketName),
		Type: "aws:s3/bucket:Bucket",
		Outputs: resource.PropertyMap{
			"bucket":       resource.NewStringProperty(bucketName),
			"arn":          resource.NewStringProperty(bucketArn),
			"id":           resource.NewStringProperty(bucketName),
			"region":       resource.NewStringProperty("us-west-2"),
			"tagsAll":      resource.NewObjectProperty(resource.PropertyMap{}),
			"forceDestroy": resource.NewBoolProperty(false),
		},
	}

	// Run the upsert
	result, err := Upsert(ctx, UpsertOptions{
		Resources:      []ResourceSpec{resourceSpec},
		ProviderName:   "aws",
		ProviderBinary: providerResult.BinaryPath,
		WorkDir:        absTestDir,
		StackName:      stackName,
		Port:           0, // auto-assign port
	})

	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Upsert reported failure: %s", result.Message)
	}

	if len(result.UpdatedResources) != 1 {
		t.Fatalf("expected 1 updated resource, got %d", len(result.UpdatedResources))
	}

	if result.UpdatedResources[0] != urn {
		t.Fatalf("expected URN %s, got %s", urn, result.UpdatedResources[0])
	}

	// Verify the resource is in the state
	if err := VerifyStateUpdate(ctx, absTestDir, stackName, []resource.URN{urn}); err != nil {
		t.Fatalf("State verification failed: %v", err)
	}

	// // Verify that preview shows no changes
	// if err := VerifyPreviewClean(ctx, absTestDir, stackName); err != nil {
	// 	t.Fatalf("Preview verification failed: %v", err)
	// }

	t.Logf("Successfully upserted resource: %s", urn)
	t.Logf("Result: %s", result.Message)
}

func TestMockProviderServer(t *testing.T) {
	ctx := context.Background()

	// Install AWS provider to get the binary path
	t.Log("Installing AWS provider...")
	providerResult, err := pulumix.InstallProvider(ctx, pulumix.InstallProviderOptions{
		Name:    "aws",
		Version: "v6.64.0", // Using a recent stable version
	})
	if err != nil {
		t.Fatalf("failed to install AWS provider: %v", err)
	}
	t.Logf("AWS provider installed at: %s", providerResult.BinaryPath)

	urn := resource.URN("urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket")
	resources := []ResourceSpec{
		{
			URN:  urn,
			ID:   resource.ID("my-bucket-id"),
			Type: "aws:s3/bucket:Bucket",
			Outputs: resource.PropertyMap{
				"bucket": resource.NewStringProperty("my-bucket-id"),
				"arn":    resource.NewStringProperty("arn:aws:s3:::my-bucket-id"),
			},
		},
	}

	// Create and start the mock provider server
	server, err := NewMockProviderServer("aws", providerResult.BinaryPath, resources, 0)
	if err != nil {
		t.Fatalf("failed to create mock provider: %v", err)
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start mock provider: %v", err)
	}
	defer server.Stop()

	if server.Port() == 0 {
		t.Fatal("server port should be assigned")
	}

	t.Logf("Mock provider started on port %d", server.Port())

	// Test GetPluginInfo
	info, err := server.GetPluginInfo(ctx, nil)
	if err != nil {
		t.Fatalf("GetPluginInfo failed: %v", err)
	}
	if info.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", info.Version)
	}

	t.Log("Mock provider server test passed")
}

// Helper functions

func initStack(t *testing.T, workDir, stackName string) {
	t.Helper()

	// Run pulumi stack init
	cmd := exec.Command("pulumi", "stack", "init", stackName)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init stack: %v", err)
	}

	// Set AWS region config (required even though we're not actually using AWS)
	cmd = exec.Command("pulumi", "config", "set", "aws:region", "us-west-2")
	cmd.Dir = workDir

	if err := cmd.Run(); err != nil {
		t.Logf("warning: failed to set aws:region config: %v", err)
	}
}

func cleanupStack(t *testing.T, workDir, stackName string) {
	t.Helper()

	// Select the stack
	cmd := exec.Command("pulumi", "stack", "select", stackName)
	cmd.Dir = workDir
	_ = cmd.Run() // Ignore errors if stack doesn't exist

	// Remove the stack
	cmd = exec.Command("pulumi", "stack", "rm", "--yes", stackName)
	cmd.Dir = workDir
	_ = cmd.Run() // Ignore errors if stack doesn't exist
}
