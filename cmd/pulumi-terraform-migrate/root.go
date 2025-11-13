package main

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "pulumi-terraform-migrate",
	Short: "A tool to help migrate from Terraform to Pulumi",
	Long:  `pulumi-terraform-migrate provides commands to assist with migrating Terraform projects to Pulumi, including resource mapping, state translation, and import stub resolution.`,
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	// Add flags and configuration here if needed
}
