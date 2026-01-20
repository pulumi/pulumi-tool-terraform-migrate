// Copyright 2016-2026, Pulumi Corporation.
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

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newUpdateProvidermapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-providermap",
		Short: "Update provider version mappings",
		Long: `Update provider version mappings between Terraform and Pulumi providers.

This is an administrative command used to maintain the provider version mapping data.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet")
		},
	}

	return cmd
}

func init() {
	// Only register this command if PULUMI_ADMIN_COMMANDS=true
	if os.Getenv("PULUMI_ADMIN_COMMANDS") == "true" {
		rootCmd.AddCommand(newUpdateProvidermapCmd())
	}
}
