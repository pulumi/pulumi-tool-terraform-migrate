package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pulumi/pulumi-terraform-migrate/pkg/pulumix"
	"github.com/pulumi/pulumi-terraform-migrate/pkg/tfmig"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/contract"
)

// Pulumi type token such as `aws:ec2/subnet:Subnet` that can be used with import.
type PulumiType string

// Attempts to convert a Terraform state to a Pulumi state directly. This only works for bridged providers.
// func ConvertTerraformStateToPulumi(terraformState *tfjson.StateResource) (PulumiResourceState, error) {
// 	contract.Assertf(terraformState != nil, "terraformState should not be nil")
// 	panic("TODO")
// }

// See if a Pulumi resource represents the same cloud resource as the Terraform resource.
// func IsMatchingResource(pulumiState PulumiResourceState, pulumiResource PulumiResource) (bool, error) {
// 	return false, nil
// }

// See [ComputeImportDiff].
type ImportDiff struct {
}

// Given a migration goal of moving resources over from Terraform to Pulumi, compute what import work remains to be
// done to move every resource from a given Terraform state to the given Pulumi stack.
func ComputeImportDiff(terraformState *tfjson.State, pulumiFolder string) ([]ImportDiff, error) {
	var differences []ImportDiff

	allTerraformResources, err := tfmig.AllResources(terraformState)
	if err != nil {
		return nil, nil
	}

	// What do we need in the diff? Just that the resource needs importing or anything else?

	for _, terraformResource := range allTerraformResources {
		// For every resource in Terraform state, find a matching Pulumi resource.

		panic(terraformResource)
	}

	return differences, nil
}

func oldMain() {
	stateFile := flag.String("json", "",
		"Path to a JSON file obtained by `terraform show -json`")
	flag.Parse()

	if *stateFile == "" {
		log.Fatalf("-json argument is required")
	}

	st, err := tfmig.LoadTerraformState(*stateFile)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Create a TypeMapper to handle Terraform to Pulumi type conversions
	typeMapper := tfmig.NewTypeMapper()

	importIDInferrer, err := tfmig.NewImportIDInferrer()
	defer func() {
		err := importIDInferrer.Close()
		contract.AssertNoErrorf(err, "importIDInferrer.Close failed")
	}()

	// Collect import tasks for all resources
	var importTasks []pulumix.ImportTask

	var failedImportCounter int = 0

	// Visit each resource in the state
	tfmig.VisitResources(st, func(res *tfjson.StateResource) error {
		fmt.Printf("Resource: %s (type: %s, mode: %s, provider: %s)\n",
			res.Address, res.Type, res.Mode, res.ProviderName)

		// Skip data sources - only process managed resources
		// TODO we need to translate data source calls as well as resources.
		if res.Mode == tfjson.DataResourceMode {
			return nil
		}

		// Get the Pulumi token for this resource
		pulumiToken, err := typeMapper.PulumiResourceTypeForState(ctx, *res)
		if err != nil {
			fmt.Printf("  WARNING: %v\n", err)
			return nil
		}

		fmt.Printf("  Pulumi token: %s\n", pulumiToken)
		if len(res.IdentityValues) > 0 {
			fmt.Printf("  Resource TF identity values: %#v\n", res.IdentityValues)
		}

		fmt.Printf("  Resource TF attribute values: %#v\n", res.AttributeValues)

		importID, err := importIDInferrer.InferImportID(res, pulumiToken)
		if err != nil {
			fmt.Printf("  FAIL to infer import ID: %#v\n", err)
			failedImportCounter++
			return nil
		}
		fmt.Printf("  Resource Import ID: %#v\n", importID)

		// Add to import tasks
		importTasks = append(importTasks, pulumix.ImportTask{
			ResourceType: tokens.Type(pulumiToken),
			ImportID:     pulumix.ImportID(importID),
			Name:         res.Name,
		})

		fmt.Println()

		return nil
	})

	// Import resources to Pulumi
	fmt.Printf("\n=== Importing %d resources to Pulumi ===\n\n", len(importTasks))
	states, sourceCode, err := pulumix.ImportResourcesToPulumi(ctx, importTasks, "typescript")
	if err != nil {
		log.Fatalf("Failed to import resources: %v", err)
	}

	fmt.Printf("Successfully imported %d resources (failures: %d)\n\n", len(states), failedImportCounter)
	fmt.Printf("Generated source code:\n")
	fmt.Printf("Language: %s\n", sourceCode.Language)
	fmt.Printf("Source:\n%s\n", sourceCode.Source)
}

func main() {
	Execute()
}
