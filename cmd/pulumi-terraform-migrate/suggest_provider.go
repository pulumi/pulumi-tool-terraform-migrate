package main

import (
	"fmt"

	"github.com/pulumi/pulumi-terraform-migrate/pkg/tfmig"
	"github.com/spf13/cobra"
)

var suggestProviderCmd = &cobra.Command{
	Use:   "suggest-provider [provider]",
	Short: "Suggest a Pulumi provider for a given Terraform provider",
	Long: `Suggests a Pulumi resource provider as a mapping target for a given Terraform provider's resources and data sources.

Example:
  pulumi-terraform-migrate suggest-provider registry.terraform.io/hashicorp/aws`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		tfProvider := args[0]

		// Map the Terraform provider to a Pulumi provider
		pulumiProvider := tfmig.GetPulumiProvider(tfProvider)
		if pulumiProvider == "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: no Pulumi provider mapping found for %s\n", tfProvider)
			return
		}

		// Get the recommended version for this provider
		version := tfmig.GetProviderVersion(pulumiProvider)

		// Output the result in the format: provider@version
		fmt.Printf("%s@%s\n", pulumiProvider, version)
	},
}

func init() {
	rootCmd.AddCommand(suggestProviderCmd)
}
