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

package pkg

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeriveComponentTypeToken(t *testing.T) {
	tests := []struct {
		moduleName string
		expected   string
	}{
		{"vpc", "terraform:module/vpc:Vpc"},
		{"s3_bucket", "terraform:module/s3Bucket:S3Bucket"},
		{"my_vpc_v2", "terraform:module/myVpcV2:MyVpcV2"},
		{"s3", "terraform:module/s3:S3"},
		{"VPC", "terraform:module/VPC:VPC"},
	}
	for _, tt := range tests {
		t.Run(tt.moduleName, func(t *testing.T) {
			result := deriveComponentTypeToken(tt.moduleName)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeModuleInstanceName(t *testing.T) {
	tests := []struct {
		moduleName string
		key        string
		expected   string
	}{
		{"vpc", "0", "vpc-0"},
		{"vpc", "1", "vpc-1"},
		{"vpc", "us-east-1", "vpc-us-east-1"},
		{"vpc", "us_east_1", "vpc-us-east-1"},
		{"buckets", "logs", "buckets-logs"},
		{"vpc", "a--b", "vpc-a-b"},
	}
	for _, tt := range tests {
		t.Run(tt.moduleName+"_"+tt.key, func(t *testing.T) {
			result := sanitizeModuleInstanceName(tt.moduleName, tt.key)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildComponentTree_SingleModule(t *testing.T) {
	tree, err := buildComponentTree(
		[]string{"module.vpc.aws_subnet.this", "module.vpc.aws_route_table.rt"},
		nil,
	)
	require.NoError(t, err)
	require.Len(t, tree, 1)
	require.Equal(t, "vpc", tree[0].name)
	require.Equal(t, "terraform:module/vpc:Vpc", tree[0].typeToken)
	require.Nil(t, tree[0].children)
}

func TestBuildComponentTree_NestedModules(t *testing.T) {
	tree, err := buildComponentTree(
		[]string{
			"module.vpc.module.subnets.aws_subnet.this",
			"module.vpc.aws_vpc.main",
		},
		nil,
	)
	require.NoError(t, err)
	require.Len(t, tree, 1)
	require.Equal(t, "vpc", tree[0].name)
	require.Len(t, tree[0].children, 1)
	require.Equal(t, "subnets", tree[0].children[0].name)
}

func TestBuildComponentTree_WithTypeOverride(t *testing.T) {
	tree, err := buildComponentTree(
		[]string{"module.vpc.aws_subnet.this"},
		map[string]string{"module.vpc": "myproject:index:VpcComponent"},
	)
	require.NoError(t, err)
	require.Equal(t, "myproject:index:VpcComponent", tree[0].typeToken)
}

func TestBuildComponentTree_IndexedModules(t *testing.T) {
	tree, err := buildComponentTree(
		[]string{
			"module.vpc[0].aws_subnet.this",
			"module.vpc[1].aws_subnet.this",
		},
		nil,
	)
	require.NoError(t, err)
	require.Len(t, tree, 2)
	require.Equal(t, "vpc-0", tree[0].resourceName)
	require.Equal(t, "vpc-1", tree[1].resourceName)
	require.Equal(t, tree[0].typeToken, tree[1].typeToken)
}

func TestBuildComponentTree_SiblingsSortedAlphabetically(t *testing.T) {
	tree, err := buildComponentTree(
		[]string{
			"module.zebra.aws_s3_bucket.this",
			"module.alpha.aws_s3_bucket.this",
		},
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, "alpha", tree[0].resourceName)
	require.Equal(t, "zebra", tree[1].resourceName)
}

func TestBuildComponentTree_Empty(t *testing.T) {
	tree, err := buildComponentTree([]string{}, nil)
	require.NoError(t, err)
	require.Len(t, tree, 0)
}

func TestBuildComponentTree_RootResourcesIgnored(t *testing.T) {
	tree, err := buildComponentTree(
		[]string{"aws_s3_bucket.this"},
		nil,
	)
	require.NoError(t, err)
	require.Len(t, tree, 0)
}

func TestBuildComponentTree_SanitizationCollision(t *testing.T) {
	_, err := buildComponentTree(
		[]string{
			`module.vpc["us-east-1"].aws_subnet.this`,
			`module.vpc["us_east_1"].aws_subnet.that`,
		},
		nil,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "collision")
}

func TestToComponents_DepthFirst(t *testing.T) {
	tree, err := buildComponentTree(
		[]string{
			"module.vpc.module.subnets.aws_subnet.this",
			"module.vpc.aws_vpc.main",
		},
		nil,
	)
	require.NoError(t, err)

	components := toComponents(tree, "")
	require.Len(t, components, 2)
	// vpc first (parent), then subnets (child)
	require.Equal(t, "vpc", components[0].Name)
	require.Equal(t, "", components[0].Parent) // top-level
	require.Equal(t, "subnets", components[1].Name)
	require.Equal(t, "terraform:module/vpc:Vpc", components[1].Parent) // child of vpc
}

func TestComponentParentForResource(t *testing.T) {
	tree, err := buildComponentTree(
		[]string{
			"module.vpc.module.subnets.aws_subnet.this",
			"module.vpc.aws_vpc.main",
		},
		nil,
	)
	require.NoError(t, err)

	// Resource in nested module
	segments := parseModuleSegments("module.vpc.module.subnets.aws_subnet.this")
	parent := componentParentForResource(tree, segments)
	require.Equal(t, "terraform:module/vpc:Vpc$terraform:module/subnets:Subnets", parent)

	// Resource in top-level module
	segments = parseModuleSegments("module.vpc.aws_vpc.main")
	parent = componentParentForResource(tree, segments)
	require.Equal(t, "terraform:module/vpc:Vpc", parent)

	// Root resource (no module)
	segments = parseModuleSegments("aws_s3_bucket.this")
	parent = componentParentForResource(tree, segments)
	require.Equal(t, "", parent)
}

func TestParseModuleSegments(t *testing.T) {
	tests := []struct {
		address  string
		expected []moduleSegment
	}{
		{
			"module.vpc.aws_subnet.this",
			[]moduleSegment{{name: "vpc"}},
		},
		{
			"module.vpc.module.subnets.aws_subnet.this",
			[]moduleSegment{{name: "vpc"}, {name: "subnets"}},
		},
		{
			"module.vpc[0].aws_subnet.this",
			[]moduleSegment{{name: "vpc", key: "0"}},
		},
		{
			`module.vpc["us-east-1"].aws_subnet.this`,
			[]moduleSegment{{name: "vpc", key: "us-east-1"}},
		},
		{
			`module.clusters[0].module.services["api"].aws_lambda_function.handler`,
			[]moduleSegment{{name: "clusters", key: "0"}, {name: "services", key: "api"}},
		},
		{
			"aws_s3_bucket.this",
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			result := parseModuleSegments(tt.address)
			require.Equal(t, tt.expected, result)
		})
	}
}
