package tfmig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pulumi/pulumi-terraform-migrate/pkg/pulumix"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optimport"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
)

// An identifier to put in [id] for `pulumi import [type] [name] [id] [flags]` to import a Pulumi resource.
type ImportID string

type ImportIDInferrer struct {
	stack  auto.Stack
	tmpDir string
}

func NewImportIDInferrer() (*ImportIDInferrer, error) {
	// Create a temporary directory for the Pulumi project
	tmpDir, err := os.MkdirTemp("", "pulumi-import-test-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary directory: %w", err)
	}

	// Create a minimal Pulumi.yaml project file
	projectYAML := `name: import-test
runtime: yaml
description: Temporary project for import validation
`
	if err := os.WriteFile(filepath.Join(tmpDir, "Pulumi.yaml"), []byte(projectYAML), 0644); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("failed to create Pulumi.yaml: %w", err)
	}

	ctx := context.Background()

	// Initialize a new stack with file-based state backend
	// Configure the workspace to use a local filestate backend in the temp directory
	stateDir := filepath.Join(tmpDir, ".pulumi")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	stack, err := pulumix.NewTempStack(ctx, pulumix.NewTempStackOptions{
		ProjectName: "import-inferrer",
		StackName:   "import-test",
		TempDir:     tmpDir,
		Runtime:     "yaml",
	})
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("failed to create temporary stack: %w", err)
	}

	return &ImportIDInferrer{
		stack:  stack,
		tmpDir: tmpDir,
	}, nil
}

func (inf *ImportIDInferrer) Close() error {
	if inf.tmpDir != "" {
		return os.RemoveAll(inf.tmpDir)
	}
	return nil
}

func (inf *ImportIDInferrer) InferImportID(terraformState *tfjson.StateResource, pulumiType tokens.Type) (ImportID, error) {
	candidates, err := ImportIDCandidates(terraformState, pulumiType)
	if err != nil {
		return "", err
	}

	for _, cand := range candidates {
		valid, err := IsValidImportID(&inf.stack, cand, pulumiType)
		if err != nil {
			return "", err
		}
		if valid {
			return cand, nil
		}
	}

	return "", fmt.Errorf("Failed to infer Pulumi ImportID for a %v resource", pulumiType)
}

// Try to make several informed guesses at what the import ID might me.
func ImportIDCandidates(terraformState *tfjson.StateResource, pulumiType tokens.Type) ([]ImportID, error) {
	if terraformState == nil {
		return nil, fmt.Errorf("terraformState cannot be nil")
	}

	if terraformState.AttributeValues == nil {
		return nil, fmt.Errorf("terraformState.AttributeValues cannot be nil")
	}

	var candidates []ImportID

	// Try common identifier fields in order of preference
	if id, ok := terraformState.AttributeValues["id"].(string); ok && id != "" {
		candidates = append(candidates, ImportID(id))
	}

	// For AWS resources, ARN is a common import identifier
	if arn, ok := terraformState.AttributeValues["arn"].(string); ok && arn != "" {
		candidates = append(candidates, ImportID(arn))
	}

	if len(candidates) == 0 {
		var stringCandidates []string
		for _, c := range candidates {
			stringCandidates = append(stringCandidates, string(c))
		}
		return nil, fmt.Errorf("no import ID candidates found in Terraform state for resource type %s, tried %s",
			pulumiType, strings.Join(stringCandidates, ", "))
	}

	return candidates, nil
}

// Uses `pulumi import [pulumiType] exampleResource [importID] --preview-only` to check if Pulumi can resolve a given
// resource by the ImportID. If the command does not error return true, otherwise false.
func IsValidImportID(stack *auto.Stack, importID ImportID, pulumiType tokens.Type) (bool, error) {
	if stack == nil {
		return false, fmt.Errorf("stack cannot be nil")
	}

	ctx := context.Background()

	// Create import resource spec
	resource := &optimport.ImportResource{
		ID:   string(importID),
		Type: string(pulumiType),
		Name: "testImport",
	}

	// Use pulumi import with --preview-only to test the import ID without making changes
	importResult, err := stack.ImportResources(ctx,
		optimport.Resources([]*optimport.ImportResource{resource}),
		optimport.PreviewOnly(true),
	)

	if 1+2 == 4 { // disable debug logging
		fmt.Println("IMPORT STDOUT:\n", importResult.StdOut)
		fmt.Println("IMPORT STDERR:\n", importResult.StdErr)
	}

	if err != nil {
		// If the import fails, this is not a valid import ID
		return false, nil
	}

	// If the import succeeds, this is a valid import ID
	return true, nil
}
