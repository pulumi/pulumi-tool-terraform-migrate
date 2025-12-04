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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadMigration(t *testing.T) {
	t.Parallel()

	t.Run("loads valid migration file", func(t *testing.T) {
		t.Parallel()

		// Create a temporary migration file
		tmpDir := t.TempDir()
		migrationPath := filepath.Join(tmpDir, "migration.json")

		content := `{
  "migration": {
    "tf-sources": "./terraform",
    "pulumi-sources": "./pulumi",
    "stacks": [
      {
        "tf-state": "terraform.tfstate",
        "pulumi-stack": "dev",
        "resources": [
          {
            "tf-addr": "aws_instance.web",
            "urn": "urn:pulumi:dev::my-project::aws:ec2/instance:Instance::web"
          },
          {
            "tf-addr": "aws_s3_bucket.data",
            "migrate": "skip"
          }
        ]
      }
    ]
  }
}`
		err := os.WriteFile(migrationPath, []byte(content), 0644)
		require.NoError(t, err)

		// Load the migration
		mf, err := LoadMigration(migrationPath)
		require.NoError(t, err)
		require.NotNil(t, mf)

		// Verify the loaded data
		assert.Equal(t, "./terraform", mf.Migration.TFSources)
		assert.Equal(t, "./pulumi", mf.Migration.PulumiSources)
		assert.Len(t, mf.Migration.Stacks, 1)

		stack := mf.Migration.Stacks[0]
		assert.Equal(t, "terraform.tfstate", stack.TFState)
		assert.Equal(t, "dev", stack.PulumiStack)
		assert.Len(t, stack.Resources, 2)

		assert.Equal(t, "aws_instance.web", stack.Resources[0].TFAddr)
		assert.Equal(t, "urn:pulumi:dev::my-project::aws:ec2/instance:Instance::web", stack.Resources[0].URN)
		assert.Equal(t, MigrateModeEmpty, stack.Resources[0].Migrate)

		assert.Equal(t, "aws_s3_bucket.data", stack.Resources[1].TFAddr)
		assert.Equal(t, "", stack.Resources[1].URN)
		assert.Equal(t, MigrateModeSkip, stack.Resources[1].Migrate)
	})

	t.Run("returns error for non-existent file", func(t *testing.T) {
		t.Parallel()

		_, err := LoadMigration("/non/existent/path/migration.json")
		assert.Error(t, err)
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		migrationPath := filepath.Join(tmpDir, "migration.json")

		err := os.WriteFile(migrationPath, []byte("invalid json"), 0644)
		require.NoError(t, err)

		_, err = LoadMigration(migrationPath)
		assert.Error(t, err)
	})
}

func TestMigrationFile_Save(t *testing.T) {
	t.Parallel()

	t.Run("saves migration file correctly", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		migrationPath := filepath.Join(tmpDir, "migration.json")

		// Create a migration file
		mf := &MigrationFile{
			Migration: Migration{
				TFSources:     "./terraform",
				PulumiSources: "./pulumi",
				Stacks: []Stack{
					{
						TFState:     "terraform.tfstate",
						PulumiStack: "prod",
						Resources: []Resource{
							{
								TFAddr: "aws_instance.app",
								URN:    "urn:pulumi:prod::my-project::aws:ec2/instance:Instance::app",
							},
							{
								TFAddr:  "aws_s3_bucket.logs",
								Migrate: MigrateModeIgnoreNoState,
							},
						},
					},
				},
			},
		}

		// Save the file
		err := mf.Save(migrationPath)
		require.NoError(t, err)

		// Verify the file exists
		_, err = os.Stat(migrationPath)
		require.NoError(t, err)

		// Load it back and verify contents
		loaded, err := LoadMigration(migrationPath)
		require.NoError(t, err)

		assert.Equal(t, mf.Migration.TFSources, loaded.Migration.TFSources)
		assert.Equal(t, mf.Migration.PulumiSources, loaded.Migration.PulumiSources)
		assert.Len(t, loaded.Migration.Stacks, 1)
		assert.Equal(t, "prod", loaded.Migration.Stacks[0].PulumiStack)
		assert.Len(t, loaded.Migration.Stacks[0].Resources, 2)
		assert.Equal(t, MigrateModeIgnoreNoState, loaded.Migration.Stacks[0].Resources[1].Migrate)
	})

	t.Run("returns error for invalid path", func(t *testing.T) {
		t.Parallel()

		mf := &MigrationFile{}
		err := mf.Save("/invalid/directory/that/does/not/exist/migration.json")
		assert.Error(t, err)
	})
}
