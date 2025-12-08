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

func TestCheckResult(t *testing.T) {
	t.Parallel()

	t.Run("HasErrors returns false for empty result", func(t *testing.T) {
		t.Parallel()
		result := &CheckResult{}
		assert.False(t, result.HasErrors())
	})

	t.Run("HasErrors returns true when errors exist", func(t *testing.T) {
		t.Parallel()
		result := &CheckResult{}
		result.AddError("test", "error message")
		assert.True(t, result.HasErrors())
	})

	t.Run("AddError adds error without suggestion", func(t *testing.T) {
		t.Parallel()
		result := &CheckResult{}
		result.AddError("category1", "message1")

		require.Len(t, result.Errors, 1)
		assert.Equal(t, "category1", result.Errors[0].Category)
		assert.Equal(t, "message1", result.Errors[0].Message)
		assert.Equal(t, "", result.Errors[0].Suggestion)
	})

	t.Run("AddErrorWithSuggestion adds error with suggestion", func(t *testing.T) {
		t.Parallel()
		result := &CheckResult{}
		result.AddErrorWithSuggestion("category2", "message2", "suggestion2")

		require.Len(t, result.Errors, 1)
		assert.Equal(t, "category2", result.Errors[0].Category)
		assert.Equal(t, "message2", result.Errors[0].Message)
		assert.Equal(t, "suggestion2", result.Errors[0].Suggestion)
	})

	t.Run("multiple errors accumulate", func(t *testing.T) {
		t.Parallel()
		result := &CheckResult{}
		result.AddError("cat1", "msg1")
		result.AddError("cat2", "msg2")
		result.AddErrorWithSuggestion("cat3", "msg3", "sug3")

		assert.Len(t, result.Errors, 3)
		assert.True(t, result.HasErrors())
	})
}

func TestCheckFilesExist(t *testing.T) {
	t.Parallel()

	t.Run("no errors when all files exist", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		tfSourcesDir := filepath.Join(tmpDir, "terraform")
		pulumiSourcesDir := filepath.Join(tmpDir, "pulumi")
		tfStateFile := filepath.Join(tmpDir, "terraform.tfstate")

		require.NoError(t, os.Mkdir(tfSourcesDir, 0755))
		require.NoError(t, os.Mkdir(pulumiSourcesDir, 0755))
		require.NoError(t, os.WriteFile(tfStateFile, []byte("{}"), 0644))

		mf := &MigrationFile{
			Migration: Migration{
				TFSources:     tfSourcesDir,
				PulumiSources: pulumiSourcesDir,
				Stacks: []Stack{
					{
						TFState:     tfStateFile,
						PulumiStack: "dev",
					},
				},
			},
		}

		result := &CheckResult{}
		checkFilesExist(mf, result)

		assert.False(t, result.HasErrors())
	})

	t.Run("error when tf-sources directory missing", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		pulumiSourcesDir := filepath.Join(tmpDir, "pulumi")
		require.NoError(t, os.Mkdir(pulumiSourcesDir, 0755))

		mf := &MigrationFile{
			Migration: Migration{
				TFSources:     filepath.Join(tmpDir, "nonexistent"),
				PulumiSources: pulumiSourcesDir,
			},
		}

		result := &CheckResult{}
		checkFilesExist(mf, result)

		assert.True(t, result.HasErrors())
		assert.Contains(t, result.Errors[0].Message, "tf-sources directory does not exist")
	})

	t.Run("error when pulumi-sources directory missing", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		tfSourcesDir := filepath.Join(tmpDir, "terraform")
		require.NoError(t, os.Mkdir(tfSourcesDir, 0755))

		mf := &MigrationFile{
			Migration: Migration{
				TFSources:     tfSourcesDir,
				PulumiSources: filepath.Join(tmpDir, "nonexistent"),
			},
		}

		result := &CheckResult{}
		checkFilesExist(mf, result)

		assert.True(t, result.HasErrors())
		assert.Contains(t, result.Errors[0].Message, "pulumi-sources directory does not exist")
	})

	t.Run("error when tf-state file missing", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		tfSourcesDir := filepath.Join(tmpDir, "terraform")
		pulumiSourcesDir := filepath.Join(tmpDir, "pulumi")
		require.NoError(t, os.Mkdir(tfSourcesDir, 0755))
		require.NoError(t, os.Mkdir(pulumiSourcesDir, 0755))

		mf := &MigrationFile{
			Migration: Migration{
				TFSources:     tfSourcesDir,
				PulumiSources: pulumiSourcesDir,
				Stacks: []Stack{
					{
						TFState:     filepath.Join(tmpDir, "nonexistent.tfstate"),
						PulumiStack: "dev",
					},
				},
			},
		}

		result := &CheckResult{}
		checkFilesExist(mf, result)

		assert.True(t, result.HasErrors())
		assert.Contains(t, result.Errors[0].Message, "tf-state file does not exist")
		assert.Contains(t, result.Errors[0].Message, "stack[0] (dev)")
	})

	t.Run("no error when tf-state is empty string", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		tfSourcesDir := filepath.Join(tmpDir, "terraform")
		pulumiSourcesDir := filepath.Join(tmpDir, "pulumi")
		require.NoError(t, os.Mkdir(tfSourcesDir, 0755))
		require.NoError(t, os.Mkdir(pulumiSourcesDir, 0755))

		mf := &MigrationFile{
			Migration: Migration{
				TFSources:     tfSourcesDir,
				PulumiSources: pulumiSourcesDir,
				Stacks: []Stack{
					{
						TFState:     "",
						PulumiStack: "dev",
					},
				},
			},
		}

		result := &CheckResult{}
		checkFilesExist(mf, result)

		assert.False(t, result.HasErrors())
	})

	t.Run("multiple stack errors reported with correct indices", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		tfSourcesDir := filepath.Join(tmpDir, "terraform")
		pulumiSourcesDir := filepath.Join(tmpDir, "pulumi")
		require.NoError(t, os.Mkdir(tfSourcesDir, 0755))
		require.NoError(t, os.Mkdir(pulumiSourcesDir, 0755))

		mf := &MigrationFile{
			Migration: Migration{
				TFSources:     tfSourcesDir,
				PulumiSources: pulumiSourcesDir,
				Stacks: []Stack{
					{
						TFState:     filepath.Join(tmpDir, "missing1.tfstate"),
						PulumiStack: "dev",
					},
					{
						TFState:     filepath.Join(tmpDir, "missing2.tfstate"),
						PulumiStack: "prod",
					},
				},
			},
		}

		result := &CheckResult{}
		checkFilesExist(mf, result)

		assert.True(t, result.HasErrors())
		assert.Len(t, result.Errors, 2)
		assert.Contains(t, result.Errors[0].Message, "stack[0] (dev)")
		assert.Contains(t, result.Errors[1].Message, "stack[1] (prod)")
	})
}

func TestCheckUniqueMapping(t *testing.T) {
	t.Parallel()

	t.Run("no errors with unique mappings", func(t *testing.T) {
		t.Parallel()

		mf := &MigrationFile{
			Migration: Migration{
				Stacks: []Stack{
					{
						PulumiStack: "dev",
						Resources: []Resource{
							{
								TFAddr: "aws_instance.web",
								URN:    "urn:pulumi:dev::proj::aws:ec2/instance:Instance::web",
							},
							{
								TFAddr: "aws_s3_bucket.data",
								URN:    "urn:pulumi:dev::proj::aws:s3/bucket:Bucket::data",
							},
						},
					},
				},
			},
		}

		result := &CheckResult{}
		checkUniqueMapping(mf, result)

		assert.False(t, result.HasErrors())
	})

	t.Run("error when tf-addr maps to multiple URNs", func(t *testing.T) {
		t.Parallel()

		mf := &MigrationFile{
			Migration: Migration{
				Stacks: []Stack{
					{
						PulumiStack: "dev",
						Resources: []Resource{
							{
								TFAddr: "aws_instance.web",
								URN:    "urn:pulumi:dev::proj::aws:ec2/instance:Instance::web1",
							},
							{
								TFAddr: "aws_instance.web",
								URN:    "urn:pulumi:dev::proj::aws:ec2/instance:Instance::web2",
							},
						},
					},
				},
			},
		}

		result := &CheckResult{}
		checkUniqueMapping(mf, result)

		assert.True(t, result.HasErrors())
		require.Len(t, result.Errors, 1)
		assert.Equal(t, "unique-mapping", result.Errors[0].Category)
		assert.Contains(t, result.Errors[0].Message, "tf-addr 'aws_instance.web' maps to multiple URNs")
		assert.Contains(t, result.Errors[0].Message, "stack[0] (dev)")
	})

	t.Run("error when URN maps to multiple tf-addrs", func(t *testing.T) {
		t.Parallel()

		mf := &MigrationFile{
			Migration: Migration{
				Stacks: []Stack{
					{
						PulumiStack: "prod",
						Resources: []Resource{
							{
								TFAddr: "aws_instance.web1",
								URN:    "urn:pulumi:prod::proj::aws:ec2/instance:Instance::web",
							},
							{
								TFAddr: "aws_instance.web2",
								URN:    "urn:pulumi:prod::proj::aws:ec2/instance:Instance::web",
							},
						},
					},
				},
			},
		}

		result := &CheckResult{}
		checkUniqueMapping(mf, result)

		assert.True(t, result.HasErrors())
		require.Len(t, result.Errors, 1)
		assert.Equal(t, "unique-mapping", result.Errors[0].Category)
		assert.Contains(t, result.Errors[0].Message, "URN")
		assert.Contains(t, result.Errors[0].Message, "maps to multiple tf-addrs")
		assert.Contains(t, result.Errors[0].Suggestion, "Edit migration.json")
	})

	t.Run("skips resources with migrate mode set", func(t *testing.T) {
		t.Parallel()

		mf := &MigrationFile{
			Migration: Migration{
				Stacks: []Stack{
					{
						PulumiStack: "dev",
						Resources: []Resource{
							{
								TFAddr:  "aws_instance.skip1",
								URN:     "urn:pulumi:dev::proj::aws:ec2/instance:Instance::web",
								Migrate: MigrateModeSkip,
							},
							{
								TFAddr:  "aws_instance.skip2",
								URN:     "urn:pulumi:dev::proj::aws:ec2/instance:Instance::web",
								Migrate: MigrateModeIgnoreNoState,
							},
						},
					},
				},
			},
		}

		result := &CheckResult{}
		checkUniqueMapping(mf, result)

		assert.False(t, result.HasErrors())
	})

	t.Run("error when resource has empty URN", func(t *testing.T) {
		t.Parallel()

		mf := &MigrationFile{
			Migration: Migration{
				Stacks: []Stack{
					{
						PulumiStack: "dev",
						Resources: []Resource{
							{
								TFAddr: "aws_instance.web",
								URN:    "",
							},
						},
					},
				},
			},
		}

		result := &CheckResult{}
		checkUniqueMapping(mf, result)

		assert.True(t, result.HasErrors())
		require.Len(t, result.Errors, 1)
		assert.Equal(t, "invalid-resource", result.Errors[0].Category)
		assert.Contains(t, result.Errors[0].Message, "has empty URN")
		assert.Contains(t, result.Errors[0].Message, "aws_instance.web")
		assert.Contains(t, result.Errors[0].Suggestion, "Add a URN mapping or set migrate: \"skip\"")
	})

	t.Run("error when resource has empty tf-addr", func(t *testing.T) {
		t.Parallel()

		mf := &MigrationFile{
			Migration: Migration{
				Stacks: []Stack{
					{
						PulumiStack: "dev",
						Resources: []Resource{
							{
								TFAddr: "",
								URN:    "urn:pulumi:dev::proj::aws:ec2/instance:Instance::web1",
							},
							{
								TFAddr: "",
								URN:    "urn:pulumi:dev::proj::aws:ec2/instance:Instance::web2",
							},
						},
					},
				},
			},
		}

		result := &CheckResult{}
		checkUniqueMapping(mf, result)

		assert.True(t, result.HasErrors())
		require.Len(t, result.Errors, 2)
		assert.Equal(t, "invalid-resource", result.Errors[0].Category)
		assert.Contains(t, result.Errors[0].Message, "has empty tf-addr")
		assert.Contains(t, result.Errors[0].Suggestion, "Remove this invalid resource entry")
	})

	t.Run("handles multiple stacks independently", func(t *testing.T) {
		t.Parallel()

		mf := &MigrationFile{
			Migration: Migration{
				Stacks: []Stack{
					{
						PulumiStack: "dev",
						Resources: []Resource{
							{
								TFAddr: "aws_instance.web",
								URN:    "urn:pulumi:dev::proj::aws:ec2/instance:Instance::web1",
							},
							{
								TFAddr: "aws_instance.web",
								URN:    "urn:pulumi:dev::proj::aws:ec2/instance:Instance::web2",
							},
						},
					},
					{
						PulumiStack: "prod",
						Resources: []Resource{
							{
								TFAddr: "aws_instance.app",
								URN:    "urn:pulumi:prod::proj::aws:ec2/instance:Instance::app",
							},
						},
					},
				},
			},
		}

		result := &CheckResult{}
		checkUniqueMapping(mf, result)

		// Should only report error for dev stack
		assert.True(t, result.HasErrors())
		require.Len(t, result.Errors, 1)
		assert.Contains(t, result.Errors[0].Message, "stack[0] (dev)")
	})
}

func TestCheckStateConsistency(t *testing.T) {
	t.Parallel()

	t.Run("no errors when resources match state", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		stateFile := filepath.Join(tmpDir, "terraform.tfstate.json")

		// Use JSON format state file that doesn't require provider schemas
		stateContent := `{
  "format_version": "1.0",
  "terraform_version": "1.0.0",
  "values": {
    "root_module": {
      "resources": [
        {
          "address": "null_resource.web",
          "mode": "managed",
          "type": "null_resource",
          "name": "web"
        },
        {
          "address": "null_resource.data",
          "mode": "managed",
          "type": "null_resource",
          "name": "data"
        }
      ]
    }
  }
}`
		require.NoError(t, os.WriteFile(stateFile, []byte(stateContent), 0644))

		mf := &MigrationFile{
			Migration: Migration{
				Stacks: []Stack{
					{
						TFState:     stateFile,
						PulumiStack: "dev",
						Resources: []Resource{
							{
								TFAddr: "null_resource.web",
								URN:    "urn:pulumi:dev::proj::null:resource:Resource::web",
							},
							{
								TFAddr: "null_resource.data",
								URN:    "urn:pulumi:dev::proj::null:resource:Resource::data",
							},
						},
					},
				},
			},
		}

		result := &CheckResult{}
		err := checkStateConsistency(mf, result)

		require.NoError(t, err)
		assert.False(t, result.HasErrors())
	})

	t.Run("skips check when tf-state is empty", func(t *testing.T) {
		t.Parallel()

		mf := &MigrationFile{
			Migration: Migration{
				Stacks: []Stack{
					{
						TFState:     "",
						PulumiStack: "dev",
						Resources: []Resource{
							{
								TFAddr: "aws_instance.web",
								URN:    "urn:pulumi:dev::proj::aws:ec2/instance:Instance::web",
							},
						},
					},
				},
			},
		}

		result := &CheckResult{}
		err := checkStateConsistency(mf, result)

		require.NoError(t, err)
		assert.False(t, result.HasErrors())
	})

	t.Run("error when resource exists in state but not in migration", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		stateFile := filepath.Join(tmpDir, "terraform.tfstate.json")

		stateContent := `{
  "format_version": "1.0",
  "terraform_version": "1.0.0",
  "values": {
    "root_module": {
      "resources": [
        {
          "address": "null_resource.web",
          "mode": "managed",
          "type": "null_resource",
          "name": "web"
        }
      ]
    }
  }
}`
		require.NoError(t, os.WriteFile(stateFile, []byte(stateContent), 0644))

		mf := &MigrationFile{
			Migration: Migration{
				Stacks: []Stack{
					{
						TFState:     stateFile,
						PulumiStack: "dev",
						Resources:   []Resource{},
					},
				},
			},
		}

		result := &CheckResult{}
		err := checkStateConsistency(mf, result)

		require.NoError(t, err)
		assert.True(t, result.HasErrors())
		require.Len(t, result.Errors, 1)
		assert.Equal(t, "state-consistency", result.Errors[0].Category)
		assert.Contains(t, result.Errors[0].Message, "exists in Terraform state but not in migration.json")
		assert.Contains(t, result.Errors[0].Message, "null_resource.web")
		assert.Contains(t, result.Errors[0].Suggestion, "Add an entry for this resource")
	})

	t.Run("error when resource exists in migration but not in state", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		stateFile := filepath.Join(tmpDir, "terraform.tfstate.json")

		stateContent := `{
  "format_version": "1.0",
  "terraform_version": "1.0.0",
  "values": {
    "root_module": {
      "resources": []
    }
  }
}`
		require.NoError(t, os.WriteFile(stateFile, []byte(stateContent), 0644))

		mf := &MigrationFile{
			Migration: Migration{
				Stacks: []Stack{
					{
						TFState:     stateFile,
						PulumiStack: "dev",
						Resources: []Resource{
							{
								TFAddr: "null_resource.web",
								URN:    "urn:pulumi:dev::proj::null:resource:Resource::web",
							},
						},
					},
				},
			},
		}

		result := &CheckResult{}
		err := checkStateConsistency(mf, result)

		require.NoError(t, err)
		assert.True(t, result.HasErrors())
		require.Len(t, result.Errors, 1)
		assert.Equal(t, "state-consistency", result.Errors[0].Category)
		assert.Contains(t, result.Errors[0].Message, "exists in migration.json but not in Terraform state")
		assert.Contains(t, result.Errors[0].Message, "null_resource.web")
		assert.Contains(t, result.Errors[0].Suggestion, "Remove this resource")
	})

	t.Run("returns error when state file does not exist", func(t *testing.T) {
		t.Parallel()

		mf := &MigrationFile{
			Migration: Migration{
				Stacks: []Stack{
					{
						TFState:     "/nonexistent/terraform.tfstate",
						PulumiStack: "dev",
					},
				},
			},
		}

		result := &CheckResult{}
		err := checkStateConsistency(mf, result)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to load state")
	})

	t.Run("returns error when state file is invalid", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		stateFile := filepath.Join(tmpDir, "terraform.tfstate")
		require.NoError(t, os.WriteFile(stateFile, []byte("invalid json"), 0644))

		mf := &MigrationFile{
			Migration: Migration{
				Stacks: []Stack{
					{
						TFState:     stateFile,
						PulumiStack: "dev",
					},
				},
			},
		}

		result := &CheckResult{}
		err := checkStateConsistency(mf, result)

		require.Error(t, err)
	})
}

func TestCheckMigrationIntegrity(t *testing.T) {
	t.Parallel()

	t.Run("passes all checks with valid migration", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		tfSourcesDir := filepath.Join(tmpDir, "terraform")
		pulumiSourcesDir := filepath.Join(tmpDir, "pulumi")
		stateFile := filepath.Join(tmpDir, "terraform.tfstate.json")

		require.NoError(t, os.Mkdir(tfSourcesDir, 0755))
		require.NoError(t, os.Mkdir(pulumiSourcesDir, 0755))

		stateContent := `{
  "format_version": "1.0",
  "terraform_version": "1.0.0",
  "values": {
    "root_module": {
      "resources": [
        {
          "address": "null_resource.web",
          "mode": "managed",
          "type": "null_resource",
          "name": "web"
        }
      ]
    }
  }
}`
		require.NoError(t, os.WriteFile(stateFile, []byte(stateContent), 0644))

		mf := &MigrationFile{
			Migration: Migration{
				TFSources:     tfSourcesDir,
				PulumiSources: pulumiSourcesDir,
				Stacks: []Stack{
					{
						TFState:     stateFile,
						PulumiStack: "dev",
						Resources: []Resource{
							{
								TFAddr: "null_resource.web",
								URN:    "urn:pulumi:dev::proj::null:resource:Resource::web",
							},
						},
					},
				},
			},
		}

		result, err := CheckMigrationIntegrity(mf)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.HasErrors())
	})

	t.Run("accumulates errors from all checks", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		stateFile := filepath.Join(tmpDir, "terraform.tfstate.json")

		stateContent := `{
  "format_version": "1.0",
  "terraform_version": "1.0.0",
  "values": {
    "root_module": {
      "resources": []
    }
  }
}`
		require.NoError(t, os.WriteFile(stateFile, []byte(stateContent), 0644))

		mf := &MigrationFile{
			Migration: Migration{
				TFSources:     "/nonexistent/terraform",
				PulumiSources: "/nonexistent/pulumi",
				Stacks: []Stack{
					{
						TFState:     stateFile,
						PulumiStack: "dev",
						Resources: []Resource{
							{
								TFAddr: "null_resource.web",
								URN:    "urn:pulumi:dev::proj::null:resource:Resource::web1",
							},
							{
								TFAddr: "null_resource.web",
								URN:    "urn:pulumi:dev::proj::null:resource:Resource::web2",
							},
							{
								TFAddr: "null_resource.missing",
								URN:    "urn:pulumi:dev::proj::null:resource:Resource::missing",
							},
						},
					},
				},
			},
		}

		result, err := CheckMigrationIntegrity(mf)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.HasErrors())
		// Should have errors from:
		// 1. Missing tf-sources directory
		// 2. Missing pulumi-sources directory
		// 3. Duplicate tf-addr mapping
		// 4. Resource in migration but not in state (2 resources)
		assert.GreaterOrEqual(t, len(result.Errors), 5)
	})

	t.Run("returns error when state check fails", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		tfSourcesDir := filepath.Join(tmpDir, "terraform")
		pulumiSourcesDir := filepath.Join(tmpDir, "pulumi")

		require.NoError(t, os.Mkdir(tfSourcesDir, 0755))
		require.NoError(t, os.Mkdir(pulumiSourcesDir, 0755))

		mf := &MigrationFile{
			Migration: Migration{
				TFSources:     tfSourcesDir,
				PulumiSources: pulumiSourcesDir,
				Stacks: []Stack{
					{
						TFState:     "/nonexistent/terraform.tfstate",
						PulumiStack: "dev",
					},
				},
			},
		}

		result, err := CheckMigrationIntegrity(mf)

		require.Error(t, err)
		assert.Nil(t, result)
	})
}
