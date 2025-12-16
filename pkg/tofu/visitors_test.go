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

package tofu

import (
	"testing"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultVisitOptions(t *testing.T) {
	t.Parallel()

	opts := &VisitOptions{}
	assert.NotNil(t, opts)
	assert.False(t, opts.IncludeDataSources, "Data sources should be skipped by default (zero value)")
}

func TestVisitResources_NilState(t *testing.T) {
	t.Parallel()

	visited := 0
	err := VisitResources(nil, func(res *tfjson.StateResource) error {
		visited++
		return nil
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, visited)
}

func TestVisitResources_EmptyState(t *testing.T) {
	t.Parallel()

	state := &tfjson.State{}
	visited := 0
	err := VisitResources(state, func(res *tfjson.StateResource) error {
		visited++
		return nil
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, visited)
}

func TestVisitResources_SkipDataSources(t *testing.T) {
	t.Parallel()

	state := &tfjson.State{
		Values: &tfjson.StateValues{
			RootModule: &tfjson.StateModule{
				Resources: []*tfjson.StateResource{
					{
						Address: "aws_instance.web",
						Mode:    tfjson.ManagedResourceMode,
						Type:    "aws_instance",
						Name:    "web",
					},
					{
						Address: "data.aws_ami.ubuntu",
						Mode:    tfjson.DataResourceMode,
						Type:    "aws_ami",
						Name:    "ubuntu",
					},
					{
						Address: "aws_s3_bucket.bucket",
						Mode:    tfjson.ManagedResourceMode,
						Type:    "aws_s3_bucket",
						Name:    "bucket",
					},
				},
			},
		},
	}

	// Test with default options (should skip data sources)
	visited := []string{}
	err := VisitResources(state, func(res *tfjson.StateResource) error {
		visited = append(visited, res.Address)
		return nil
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"aws_instance.web", "aws_s3_bucket.bucket"}, visited)
}

func TestVisitResources_IncludeDataSources(t *testing.T) {
	t.Parallel()

	state := &tfjson.State{
		Values: &tfjson.StateValues{
			RootModule: &tfjson.StateModule{
				Resources: []*tfjson.StateResource{
					{
						Address: "aws_instance.web",
						Mode:    tfjson.ManagedResourceMode,
						Type:    "aws_instance",
						Name:    "web",
					},
					{
						Address: "data.aws_ami.ubuntu",
						Mode:    tfjson.DataResourceMode,
						Type:    "aws_ami",
						Name:    "ubuntu",
					},
				},
			},
		},
	}

	// Test with IncludeDataSources = true
	visited := []string{}
	err := VisitResources(state, func(res *tfjson.StateResource) error {
		visited = append(visited, res.Address)
		return nil
	}, &VisitOptions{IncludeDataSources: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"aws_instance.web", "data.aws_ami.ubuntu"}, visited)
}

func TestVisitResources_WithChildModules(t *testing.T) {
	t.Parallel()

	state := &tfjson.State{
		Values: &tfjson.StateValues{
			RootModule: &tfjson.StateModule{
				Resources: []*tfjson.StateResource{
					{
						Address: "aws_instance.root",
						Mode:    tfjson.ManagedResourceMode,
						Type:    "aws_instance",
						Name:    "root",
					},
				},
				ChildModules: []*tfjson.StateModule{
					{
						Address: "module.child1",
						Resources: []*tfjson.StateResource{
							{
								Address: "module.child1.aws_s3_bucket.bucket",
								Mode:    tfjson.ManagedResourceMode,
								Type:    "aws_s3_bucket",
								Name:    "bucket",
							},
						},
						ChildModules: []*tfjson.StateModule{
							{
								Address: "module.child1.module.grandchild",
								Resources: []*tfjson.StateResource{
									{
										Address: "module.child1.module.grandchild.aws_vpc.vpc",
										Mode:    tfjson.ManagedResourceMode,
										Type:    "aws_vpc",
										Name:    "vpc",
									},
								},
							},
						},
					},
					{
						Address: "module.child2",
						Resources: []*tfjson.StateResource{
							{
								Address: "module.child2.aws_subnet.subnet",
								Mode:    tfjson.ManagedResourceMode,
								Type:    "aws_subnet",
								Name:    "subnet",
							},
						},
					},
				},
			},
		},
	}

	visited := []string{}
	err := VisitResources(state, func(res *tfjson.StateResource) error {
		visited = append(visited, res.Address)
		return nil
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"aws_instance.root",
		"module.child1.aws_s3_bucket.bucket",
		"module.child1.module.grandchild.aws_vpc.vpc",
		"module.child2.aws_subnet.subnet",
	}, visited)
}

func TestVisitResources_VisitorError(t *testing.T) {
	t.Parallel()

	state := &tfjson.State{
		Values: &tfjson.StateValues{
			RootModule: &tfjson.StateModule{
				Resources: []*tfjson.StateResource{
					{
						Address: "aws_instance.web1",
						Mode:    tfjson.ManagedResourceMode,
					},
					{
						Address: "aws_instance.web2",
						Mode:    tfjson.ManagedResourceMode,
					},
				},
			},
		},
	}

	visited := []string{}
	err := VisitResources(state, func(res *tfjson.StateResource) error {
		visited = append(visited, res.Address)
		if res.Address == "aws_instance.web1" {
			return assert.AnError
		}
		return nil
	}, nil)

	require.Error(t, err)
	assert.Equal(t, []string{"aws_instance.web1"}, visited)
}
