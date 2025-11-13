package tfmig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optimport"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
)

// ImportFile represents the structure of import.json and import-stub.json
//
// The canonical structure is private so cannot be imported here:
// https://github.com/pulumi/pulumi/blob/e00d20f2724b0b0b8f5ac27cc4fe46a7bf957d9f/pkg/cmd/pulumi/operations/import.go#L169
type ImportFile struct {
	NameTable map[string]resource.URN     `json:"nameTable,omitempty"`
	Resources []*optimport.ImportResource `json:"resources"`
}

type ResolveImportStubsOptions struct {
	MigrationFile string
	StackName     string
	StubsFile     string
}

type ResolveImportStubsResult struct {
	ImportFile      ImportFile
	SkipCount       int
	ResolvedCount   int
	UnresolvedCount int
	TotalCount      int
}

func ResolveImportStubs(opts ResolveImportStubsOptions) (*ResolveImportStubsResult, error) {
	// Load migration file
	migrationData, err := LoadMigration(opts.MigrationFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load migration file: %w", err)
	}

	projectName, err := ReadPulumiProjectName(migrationData.Migration.PulumiSources)
	if err != nil {
		return nil, err
	}

	// Find the stack configuration
	var stackConfig *Stack
	for i := range migrationData.Migration.Stacks {
		if migrationData.Migration.Stacks[i].PulumiStack == opts.StackName {
			stackConfig = &migrationData.Migration.Stacks[i]
			break
		}
	}
	if stackConfig == nil {
		return nil, fmt.Errorf("stack %q not found in migration file", opts.StackName)
	}

	// Load Terraform state
	tfState, err := LoadTerraformState(stackConfig.TFState)
	if err != nil {
		return nil, fmt.Errorf("failed to load Terraform state file: %w", err)
	}

	// Load import stub file
	stubData, err := os.ReadFile(opts.StubsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read stub file: %w", err)
	}
	var stubs ImportFile
	if err := json.Unmarshal(stubData, &stubs); err != nil {
		return nil, fmt.Errorf("failed to parse stub file: %w", err)
	}

	// Build a map of URN to resource config from migration file
	urnToResource := make(map[string]*Resource)
	for i := range stackConfig.Resources {
		res := &stackConfig.Resources[i]
		if res.URN != "" {
			urnToResource[res.URN] = res
		}
	}

	// Build a map of Terraform addresses to state resources
	tfAddrToState := make(map[string]*tfjson.StateResource)
	allResources, err := AllResources(tfState)
	if err != nil {
		return nil, fmt.Errorf("failed to get all resources from Terraform state: %w", err)
	}
	for _, res := range allResources {
		tfAddrToState[res.Address] = res
	}

	// Create ImportIDInferrer
	inferrer, err := NewImportIDInferrer()
	if err != nil {
		return nil, fmt.Errorf("failed to create import ID inferrer: %w", err)
	}
	defer inferrer.Close()

	// Process each stub resource
	var resolvedResources []*optimport.ImportResource
	var skippedCount int
	var unresolvedCount int

	// First pass: lightweight checks to filter resources
	type validatedStub struct {
		stub       *optimport.ImportResource
		tfResource *tfjson.StateResource
	}
	var validatedStubs []validatedStub

	for _, stub := range stubs.Resources {
		// Find the corresponding resource in migration config by matching URN pattern
		resourceConfig, err := findMatchingResource(stackConfig.Resources, stub, opts.MigrationFile, opts.StackName, projectName)
		if err != nil {
			return nil, err
		}

		// Component resources such as awsx:ec2:Vpc. These are typically skipped but if they are not included
		// in the import file but featured as parents of other resources, they Pulumi CLI will complain and
		// refuse to import based on the import file.
		if stub.Component {
			validatedStubs = append(validatedStubs, validatedStub{
				stub: stub,
			})
			continue
		}

		// Check if resource is marked as skipped
		if resourceConfig.Migrate == "skip" {
			fmt.Fprintf(os.Stderr, "Skipping resource %s (marked as skip in migration config)\n", stub.Name)
			skippedCount++
			continue
		}

		tfAddr := resourceConfig.TFAddr

		if tfAddr == "" {
			fmt.Fprintf(os.Stderr, "Warning: Could not find Terraform resource for %s\n", stub.Name)
			unresolvedCount++
			continue
		}

		// If we have a Terraform address, validate it exists in state
		tfResource := tfAddrToState[tfAddr]
		if tfResource == nil {
			fmt.Fprintf(os.Stderr, "Warning: Terraform resource not found in state: %s\n", tfAddr)
			unresolvedCount++
			continue
		}

		// Resource passed all lightweight checks
		validatedStubs = append(validatedStubs, validatedStub{
			stub:       stub,
			tfResource: tfResource,
		})
	}

	// Second pass: expensive import ID inference on validated resources only
	for _, validated := range validatedStubs {
		stub := validated.stub
		tfResource := validated.tfResource

		// Preserve components as-is through this pass.
		if stub.Component {
			resolvedResources = append(resolvedResources, stub)
		}

		// Try to infer the import ID
		pulumiType := tokens.Type(stub.Type)
		fmt.Fprintf(os.Stderr, "Attempting to infer import ID for %s...\n", stub.Name)
		importID, err := inferrer.InferImportID(tfResource, pulumiType)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to infer import ID for %s: %v\n", stub.Name, err)
			unresolvedCount++
			continue
		}

		// Create resolved resource entry
		var resolved optimport.ImportResource
		resolved = *stub // take a copy
		resolved.ID = string(importID)
		resolvedResources = append(resolvedResources, &resolved)
		fmt.Fprintf(os.Stderr, "Resolved import ID for %s: %s\n", stub.Name, importID)
	}

	// Write output file
	output := ImportFile{
		NameTable: stubs.NameTable,
		Resources: resolvedResources,
	}

	return &ResolveImportStubsResult{
		ImportFile:      output,
		TotalCount:      len(stubs.Resources),
		ResolvedCount:   len(resolvedResources),
		SkipCount:       skippedCount,
		UnresolvedCount: unresolvedCount,
	}, nil
}

// IsMatchingResource checks if a Resource from migration.json matches an import record
func IsMatchingResource(res Resource, importRecord *optimport.ImportResource) (bool, error) {
	if res.URN == "" {
		return false, nil
	}
	urn, err := resource.ParseURN(res.URN)
	if err != nil {
		return false, err
	}

	// This code parses import records from `pulumi preview --import-file file.json`. Surprisingly there is some
	// code in Pulumi CLI that ends up disambiguating and mangling names when constructing this import file
	// compared to what is written in the program and lands in URNs. Thankfully LogicalName preserves the original
	// so it can be used here.
	//
	// https://github.com/pulumi/pulumi/blob/e00d20f2724b0b0b8f5ac27cc4fe46a7bf957d9f/pkg/cmd/pulumi/operations/preview.go#L111
	importRecordName := importRecord.Name
	if importRecord.LogicalName != importRecord.Name && importRecord.LogicalName != "" {
		importRecordName = importRecord.LogicalName
	}

	ok := urn.Name() == importRecordName &&
		urn.Type() == tokens.Type(importRecord.Type)
	return ok, nil
}

func IsPartialMatch(res Resource, importRecord *optimport.ImportResource) bool {
	if res.URN == "" {
		return false
	}
	urn, err := resource.ParseURN(res.URN)
	if err != nil {
		return false
	}

	// Partial match: type matches but name doesn't
	return urn.Type() == tokens.Type(importRecord.Type) && urn.Name() != importRecord.Name
}

func DeduceURN(projectName, stackName string, importRecord *optimport.ImportResource) string {
	name := importRecord.Name
	if importRecord.LogicalName != "" {
		name = importRecord.LogicalName
	}
	ty := importRecord.Type
	return fmt.Sprintf("urn:pulumi:%s::%s::%s::%s", stackName, projectName, ty, name)

}

type noMatchingResourceError struct {
	migrationJsonFilePath   string
	projectName             string
	stackName               string
	importRecord            *optimport.ImportResource
	partiallyMatchingRecods []Resource
}

type ambiguousMappingError struct {
	migrationJsonFilePath string
	stackName             string
	importRecord          *optimport.ImportResource
	matchingResources     []Resource
}

func (e *noMatchingResourceError) Error() string {
	urn := DeduceURN(e.projectName, e.stackName, e.importRecord)

	var exampleMatchingEntry Resource = Resource{
		TFAddr: "(terraform-resource-address)",
		URN:    urn,
	}

	var exampleSkipEntry Resource = Resource{
		URN:     urn,
		Migrate: "skip",
	}

	var b bytes.Buffer

	importRecordJSON, _ := json.MarshalIndent(e.importRecord, "  ", "  ")

	fmt.Fprintf(&b, "The migration file at %s needs stack %q to record how to map this Pulumi resource:\n\n",
		e.migrationJsonFilePath, e.stackName)
	fmt.Fprintf(&b, "  %s\n\n", string(importRecordJSON))

	// Show example matching entry
	fmt.Fprintf(&b, "If this Pulumi resource represents a Terraform resource, add an entry for it:\n\n")
	matchingJSON, _ := json.MarshalIndent(exampleMatchingEntry, "  ", "  ")
	fmt.Fprintf(&b, "  %s\n\n", string(matchingJSON))

	// Show example skip entry
	fmt.Fprintf(&b, "If this Pulumi resource needs to be ignored during the migration, add a skip entry for it:\n\n")
	skipJSON, _ := json.MarshalIndent(exampleSkipEntry, "  ", "  ")
	fmt.Fprintf(&b, "  %s\n", string(skipJSON))

	// Show partially matching records if any
	if len(e.partiallyMatchingRecods) > 0 {
		fmt.Fprintf(&b, "\nNote: %d record(s) match on type but not name, fix any mis-spelled names with:\n\n",
			len(e.partiallyMatchingRecods))
		for _, r := range e.partiallyMatchingRecods {
			partialJSON, _ := json.MarshalIndent(r, "  ", "  ")
			fmt.Fprintf(&b, "  %s\n", string(partialJSON))
		}
		fmt.Fprintf(&b, "\n")
		fmt.Fprintf(&b, "If this is the problem it can be fixed with:\n")
		fmt.Fprintf(&b, "\n")
		fmt.Fprintf(&b, "  pulumi-terraform-migrate set-urn "+
			"--migration migration.json "+
			"--tf-addr [tf-resource-address]"+
			" --urn [pulumi-resource-urn]\n\n")
	}

	return b.String()
}

func (e *ambiguousMappingError) Error() string {
	var b bytes.Buffer

	importRecordJSON, _ := json.MarshalIndent(e.importRecord, "  ", "  ")

	fmt.Fprintf(&b, "The migration file at %s has ambiguous mapping for this Pulumi resource:\n\n",
		e.migrationJsonFilePath)
	fmt.Fprintf(&b, "  %s\n\n", string(importRecordJSON))

	fmt.Fprintf(&b, "Multiple Terraform resources match this Pulumi resource (%d matches found):\n\n", len(e.matchingResources))
	for i, res := range e.matchingResources {
		resJSON, _ := json.MarshalIndent(res, "  ", "  ")
		fmt.Fprintf(&b, "  Match %d:\n", i+1)
		fmt.Fprintf(&b, "  %s\n\n", string(resJSON))
	}

	fmt.Fprintf(&b, "To resolve this issue, decide which Terraform resource the Pulumi resource represents ")
	fmt.Fprintf(&b, "and update the mappings in %s accordingly. ", e.migrationJsonFilePath)
	fmt.Fprintf(&b, "Remove or correct the duplicate entries so that only one mapping remains for this Pulumi resource.\n")

	return b.String()
}

func findMatchingResource(resources []Resource, importRecord *optimport.ImportResource, migrationFilePath, stackName, projectName string) (Resource, error) {
	var result Resource
	var matches []Resource
	var partialMatches []Resource

	for _, r := range resources {
		ok, err := IsMatchingResource(r, importRecord)
		if err != nil {
			return Resource{}, err
		}
		if ok {
			result = r
			matches = append(matches, r)
		} else if IsPartialMatch(r, importRecord) {
			partialMatches = append(partialMatches, r)
		}
	}
	switch len(matches) {
	case 0:
		return Resource{}, &noMatchingResourceError{
			migrationJsonFilePath:   migrationFilePath,
			projectName:             projectName,
			stackName:               stackName,
			importRecord:            importRecord,
			partiallyMatchingRecods: partialMatches,
		}
	case 1:
		return result, nil
	default:
		return Resource{}, &ambiguousMappingError{
			migrationJsonFilePath: migrationFilePath,
			stackName:             stackName,
			importRecord:          importRecord,
			matchingResources:     matches,
		}
	}
}
