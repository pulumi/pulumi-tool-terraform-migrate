package main

import (
	"context"
	"fmt"

	"github.com/pulumi/pulumi-terraform-migrate/pkg/tfmig"
	"github.com/spf13/cobra"
)

var suggestResourceCmd = &cobra.Command{
	Use:   "suggest-resource [provider] [resource]",
	Short: "Suggest a Pulumi resource for a given Terraform resource",
	Long: `Suggests a specific Pulumi resource as a mapping target for a given Terraform provider's resource.

Example:
  pulumi-terraform-migrate suggest-resource registry.terraform.io/hashicorp/aws aws_acm_certificate`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		tfProvider := args[0]
		tfResourceType := args[1]

		ctx := context.Background()

		// Create a TypeMapper to handle Terraform to Pulumi type conversions
		typeMapper := tfmig.NewTypeMapper()

		// Get the Pulumi resource type token for this Terraform resource
		pulumiToken, err := typeMapper.PulumiResourceType(ctx, tfProvider, tfResourceType)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n", err)
			return
		}

		// Output the result
		fmt.Printf("%s\n", pulumiToken)
	},
}

func init() {
	rootCmd.AddCommand(suggestResourceCmd)
}
