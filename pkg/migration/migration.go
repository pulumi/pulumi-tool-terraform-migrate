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

package migration

import (
	"encoding/json"
	"os"
)

// MigrateMode represents the migration status or handling of a resource
type MigrateMode string

const (
	// MigrateModeEmpty indicates the resource should be migrated normally
	MigrateModeEmpty MigrateMode = ""
	// MigrateModeSkip indicates the resource should be skipped in the migration
	MigrateModeSkip MigrateMode = "skip"
	// MigrateModeIgnoreNoState indicates that a resource that did not finish migrating state can be skipped
	MigrateModeIgnoreNoState MigrateMode = "ignore-no-state"
	// MigrateModeIgnoreNeedsUpdate indicates the resource that has state but wants to update on preview can be skipped
	MigrateModeIgnoreNeedsUpdate MigrateMode = "ignore-needs-update"
	// MigrateModeIgnoreNeedsUpdate indicates the resource that has state but wants to replace on preview can be skipped
	MigrateModeIgnoreNeedsReplace MigrateMode = "ignore-needs-replace"
)

// MigrationFile represents the top-level structure of migration.json
type MigrationFile struct {
	Migration Migration `json:"migration"`
}

// Migration contains the configuration for migrating from Terraform to Pulumi
type Migration struct {
	// Path to the Terraform sources.
	TFSources string `json:"tf-sources"`

	// Path to the Pulumi sources.
	PulumiSources string `json:"pulumi-sources"`

	// Lists of Pulumi stacks corresponding to Terraform workspaces.
	Stacks []Stack `json:"stacks"`
}

// Stack represents a mapping between a Terraform state and a Pulumi stack
type Stack struct {
	// File path to a Terraform state file. It can be in JSON format (ends with .json), or in raw binary format
	// (ends with .tfstate).
	TFState string `json:"tf-state"`

	// Name of the Pulumi stack such as "dev".
	PulumiStack string `json:"pulumi-stack"`

	// Resource mappings.
	Resources []Resource `json:"resources"`
}

// Resource represents a mapping between a Terraform resource and a Pulumi resource
type Resource struct {
	// Terraform resource address such as "aws_instance.app_server" or "aws_instance.web[0]".
	TFAddr string `json:"tf-addr,omitempty"`

	// Pulumi Resource URN such as "urn:pulumi:my-org:my-stack::my-project::aws:s3/bucket:Bucket::my-bucket" This
	// may be empty if the resource is skipped from the migration.
	URN string `json:"urn,omitempty"`

	// Encode how the particular Terraform resource should be migrated, can it be skipped completely or can certain
	// checks for this resource be ignored.
	Migrate MigrateMode `json:"migrate,omitempty"`
}

// LoadMigration reads and parses a migration.json file
func LoadMigration(path string) (*MigrationFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var mf MigrationFile
	if err := json.Unmarshal(data, &mf); err != nil {
		return nil, err
	}

	return &mf, nil
}

// Save writes the migration file to disk
func (mf *MigrationFile) Save(path string) error {
	data, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}
