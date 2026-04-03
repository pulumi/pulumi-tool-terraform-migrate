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
