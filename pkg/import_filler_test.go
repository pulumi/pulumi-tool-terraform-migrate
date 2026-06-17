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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFillImportFile_SingleMatch(t *testing.T) {
	t.Parallel()

	digest := &ModuleMap{
		Modules: map[string]*ModuleMapEntry{
			"vpc": {
				TerraformPath: "module.vpc",
				Resources: []ModuleResource{
					{
						Mode:             "managed",
						TranslatedURN:    "urn:pulumi:dev::proj::aws:ec2/vpc:Vpc::vpc-main",
						TerraformAddress: "module.vpc.aws_vpc.main",
						ImportID:         "vpc-12345",
					},
					{
						Mode:             "managed",
						TranslatedURN:    "urn:pulumi:dev::proj::aws:ec2/subnet:Subnet::vpc-public_0",
						TerraformAddress: "module.vpc.aws_subnet.public[0]",
						ImportID:         "subnet-aaa",
					},
				},
			},
		},
	}

	importFile := &ImportFile{
		Resources: []ImportEntry{
			{Type: "veridos:network:Vpc", Name: "vpc", Component: true},
			// Names follow ${parent}-${tfResourceName} convention
			{Type: "aws:ec2/vpc:Vpc", Name: "vpc-main", ID: "<PLACEHOLDER>", Parent: "vpc"},
			{Type: "aws:ec2/subnet:Subnet", Name: `vpc-public[0]`, ID: "<PLACEHOLDER>", Parent: "vpc"},
		},
	}

	mappings := map[string]string{
		"module.vpc": "vpc",
	}

	result := FillImportFile(digest, importFile, mappings, nil)

	assert.Equal(t, 2, result.Filled)
	assert.Equal(t, 1, result.Skipped)
	assert.Equal(t, 0, result.Unmatched)
	assert.Empty(t, result.Warnings)

	assert.Equal(t, "vpc-12345", importFile.Resources[1].ID)
	assert.Equal(t, "subnet-aaa", importFile.Resources[2].ID)
}

func TestFillImportFile_NameMatchMultipleSameType(t *testing.T) {
	t.Parallel()

	// Two resources of the same type — matched by name, not disambiguation.
	digest := &ModuleMap{
		Modules: map[string]*ModuleMapEntry{
			"rds": {
				TerraformPath: "module.rds",
				Resources: []ModuleResource{
					{
						Mode:             "managed",
						TranslatedURN:    "urn:pulumi:dev::proj::aws:rds/cluster:Cluster::rds-primary",
						TerraformAddress: "module.rds.aws_rds_cluster.primary",
						ImportID:         "cluster-primary",
					},
					{
						Mode:             "managed",
						TranslatedURN:    "urn:pulumi:dev::proj::aws:rds/cluster:Cluster::rds-replica",
						TerraformAddress: "module.rds.aws_rds_cluster.replica",
						ImportID:         "cluster-replica",
					},
				},
			},
		},
	}

	importFile := &ImportFile{
		Resources: []ImportEntry{
			{Type: "veridos:data:RdsCluster", Name: "rds", Component: true},
			{Type: "aws:rds/cluster:Cluster", Name: "rds-primary", ID: "<PLACEHOLDER>", Parent: "rds"},
			{Type: "aws:rds/cluster:Cluster", Name: "rds-replica", ID: "<PLACEHOLDER>", Parent: "rds"},
		},
	}

	mappings := map[string]string{
		"module.rds": "rds",
	}

	result := FillImportFile(digest, importFile, mappings, nil)

	assert.Equal(t, 2, result.Filled)
	assert.Equal(t, 0, result.Unmatched)
	assert.Empty(t, result.Warnings)
	assert.Equal(t, "cluster-primary", importFile.Resources[1].ID)
	assert.Equal(t, "cluster-replica", importFile.Resources[2].ID)
}

func TestFillImportFile_TypeOnlyFallback(t *testing.T) {
	t.Parallel()

	// Names don't match convention but there's only one of each type → fallback works.
	digest := &ModuleMap{
		Modules: map[string]*ModuleMapEntry{
			"vpc": {
				TerraformPath: "module.vpc",
				Resources: []ModuleResource{
					{
						Mode:             "managed",
						TranslatedURN:    "urn:pulumi:dev::proj::aws:ec2/vpc:Vpc::vpc-main",
						TerraformAddress: "module.vpc.aws_vpc.main",
						ImportID:         "vpc-12345",
					},
				},
			},
		},
	}

	importFile := &ImportFile{
		Resources: []ImportEntry{
			{Type: "veridos:network:Vpc", Name: "vpc", Component: true},
			// Name doesn't follow convention (legacy component)
			{Type: "aws:ec2/vpc:Vpc", Name: "vpc-my-custom-name", ID: "<PLACEHOLDER>", Parent: "vpc"},
		},
	}

	mappings := map[string]string{
		"module.vpc": "vpc",
	}

	result := FillImportFile(digest, importFile, mappings, nil)

	assert.Equal(t, 1, result.Filled)
	assert.Equal(t, 0, result.Unmatched)
	assert.Equal(t, "vpc-12345", importFile.Resources[1].ID)
}

func TestFillImportFile_RootResources(t *testing.T) {
	t.Parallel()

	digest := &ModuleMap{
		Modules: map[string]*ModuleMapEntry{},
		RootResources: []ModuleResource{
			{
				Mode:             "managed",
				TranslatedURN:    "urn:pulumi:dev::proj::aws:s3/bucket:Bucket::my_bucket",
				TerraformAddress: "aws_s3_bucket.my_bucket",
				ImportID:         "my-bucket-id",
			},
		},
	}

	importFile := &ImportFile{
		Resources: []ImportEntry{
			{Type: "aws:s3/bucket:Bucket", Name: "my_bucket", ID: "<PLACEHOLDER>"},
		},
	}

	result := FillImportFile(digest, importFile, nil, nil)

	assert.Equal(t, 1, result.Filled)
	assert.Equal(t, 0, result.Unmatched)
	assert.Equal(t, "my-bucket-id", importFile.Resources[0].ID)
}

func TestFillImportFile_MissingModule(t *testing.T) {
	t.Parallel()

	digest := &ModuleMap{
		Modules: map[string]*ModuleMapEntry{},
	}

	importFile := &ImportFile{
		Resources: []ImportEntry{
			{Type: "veridos:data:Rds", Name: "rds", Component: true},
			{Type: "aws:rds/cluster:Cluster", Name: "rds-aurora_cluster", ID: "<PLACEHOLDER>", Parent: "rds"},
		},
	}

	mappings := map[string]string{
		"module.rds": "rds",
	}

	result := FillImportFile(digest, importFile, mappings, nil)

	assert.Equal(t, 0, result.Filled)
	assert.Equal(t, 1, result.Unmatched)
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "not found in digest")
}

func TestFillImportFile_DataSourcesSkipped(t *testing.T) {
	t.Parallel()

	digest := &ModuleMap{
		Modules: map[string]*ModuleMapEntry{
			"vpc": {
				TerraformPath: "module.vpc",
				Resources: []ModuleResource{
					{
						Mode:             "managed",
						TranslatedURN:    "urn:pulumi:dev::proj::aws:ec2/vpc:Vpc::vpc-main",
						TerraformAddress: "module.vpc.aws_vpc.main",
						ImportID:         "vpc-123",
					},
					{
						Mode:             "data",
						TranslatedURN:    "",
						TerraformAddress: "module.vpc.data.aws_availability_zones.available",
						ImportID:         "",
					},
				},
			},
		},
	}

	importFile := &ImportFile{
		Resources: []ImportEntry{
			{Type: "veridos:network:Vpc", Name: "vpc", Component: true},
			{Type: "aws:ec2/vpc:Vpc", Name: "vpc-main", ID: "<PLACEHOLDER>", Parent: "vpc"},
		},
	}

	mappings := map[string]string{
		"module.vpc": "vpc",
	}

	result := FillImportFile(digest, importFile, mappings, nil)

	assert.Equal(t, 1, result.Filled)
	assert.Equal(t, 0, result.Unmatched)
	assert.Equal(t, "vpc-123", importFile.Resources[1].ID)
}

func TestFillImportFile_PrefilledIDsUntouched(t *testing.T) {
	t.Parallel()

	digest := &ModuleMap{
		Modules: map[string]*ModuleMapEntry{
			"vpc": {
				TerraformPath: "module.vpc",
				Resources: []ModuleResource{
					{
						Mode:             "managed",
						TranslatedURN:    "urn:pulumi:dev::proj::aws:ec2/vpc:Vpc::vpc-main",
						TerraformAddress: "module.vpc.aws_vpc.main",
						ImportID:         "vpc-999",
					},
				},
			},
		},
	}

	importFile := &ImportFile{
		Resources: []ImportEntry{
			{Type: "veridos:network:Vpc", Name: "vpc", Component: true},
			{Type: "aws:ec2/vpc:Vpc", Name: "vpc-main", ID: "vpc-already-set", Parent: "vpc"},
		},
	}

	mappings := map[string]string{
		"module.vpc": "vpc",
	}

	result := FillImportFile(digest, importFile, mappings, nil)

	assert.Equal(t, 0, result.Filled)
	assert.Equal(t, 0, result.Unmatched)
	assert.Equal(t, "vpc-already-set", importFile.Resources[1].ID)
}

func TestFillImportFile_ForEachMapping(t *testing.T) {
	t.Parallel()

	digest := &ModuleMap{
		Modules: map[string]*ModuleMapEntry{
			`capture_ui["dmvhm"]`: {
				TerraformPath: `module.capture_ui["dmvhm"]`,
				Resources: []ModuleResource{
					{
						Mode:             "managed",
						TranslatedURN:    `urn:pulumi:dev::proj::aws:s3/bucket:Bucket::capture_ui["dmvhm"]-ui`,
						TerraformAddress: `module.capture_ui["dmvhm"].aws_s3_bucket.ui`,
						ImportID:         "dmvhm-ui-bucket",
					},
				},
			},
		},
	}

	importFile := &ImportFile{
		Resources: []ImportEntry{
			{Type: "veridos:compute:CaptureUi", Name: `capture_ui["dmvhm"]`, Component: true},
			// Name follows convention: ${parent}-${tfResourceName}
			{Type: "aws:s3/bucket:Bucket", Name: `capture_ui["dmvhm"]-ui`, ID: "<PLACEHOLDER>", Parent: `capture_ui["dmvhm"]`},
		},
	}

	mappings := map[string]string{
		`module.capture_ui["dmvhm"]`: `capture_ui["dmvhm"]`,
	}

	result := FillImportFile(digest, importFile, mappings, nil)

	assert.Equal(t, 1, result.Filled)
	assert.Equal(t, 0, result.Unmatched)
	assert.Equal(t, "dmvhm-ui-bucket", importFile.Resources[1].ID)
}

func TestFillImportFile_ResourceMappings(t *testing.T) {
	t.Parallel()

	digest := &ModuleMap{
		Modules: map[string]*ModuleMapEntry{},
		RootResources: []ModuleResource{
			{
				Mode:             "managed",
				TranslatedURN:    "urn:pulumi:dev::proj::aws:s3/bucketV2:BucketV2::cm_cfn",
				TerraformAddress: `aws_s3_bucket.cm_cfn["my-service-develop"]`,
				ImportID:         "my-service-develop-bucket",
			},
			{
				Mode:             "managed",
				TranslatedURN:    "urn:pulumi:dev::proj::aws:s3/bucketV2:BucketV2::other_bucket",
				TerraformAddress: "aws_s3_bucket.other_bucket",
				ImportID:         "other-bucket-id",
			},
		},
	}

	importFile := &ImportFile{
		Resources: []ImportEntry{
			// Root resource with a name that doesn't match TF convention
			{Type: "aws:s3/bucketV2:BucketV2", Name: `cm_cfn["my-service-develop"]`, ID: "<PLACEHOLDER>"},
			// Root resource that matches by name (handled by root matching)
			{Type: "aws:s3/bucketV2:BucketV2", Name: "other_bucket", ID: "<PLACEHOLDER>"},
		},
	}

	resourceMappings := map[string]string{
		`aws_s3_bucket.cm_cfn["my-service-develop"]`: `cm_cfn["my-service-develop"]`,
	}

	result := FillImportFile(digest, importFile, nil, resourceMappings)

	assert.Equal(t, 2, result.Filled) // 1 from resource mapping + 1 from root matching
	assert.Equal(t, 0, result.Unmatched)
	assert.Equal(t, "my-service-develop-bucket", importFile.Resources[0].ID)
	assert.Equal(t, "other-bucket-id", importFile.Resources[1].ID)
}

func TestFillImportFile_ResourceMappingsFromModule(t *testing.T) {
	t.Parallel()

	// Resource-level mapping can also target resources inside TF modules.
	digest := &ModuleMap{
		Modules: map[string]*ModuleMapEntry{
			"rds": {
				TerraformPath: "module.rds",
				Resources: []ModuleResource{
					{
						Mode:             "managed",
						TranslatedURN:    "urn:pulumi:dev::proj::aws:rds/clusterInstance:ClusterInstance::rds-instance-0",
						TerraformAddress: `module.rds.aws_rds_cluster_instance.instance["writer"]`,
						ImportID:         "rds-writer-instance",
					},
				},
			},
		},
	}

	importFile := &ImportFile{
		Resources: []ImportEntry{
			{Type: "my:component:Rds", Name: "rds", Component: true},
			{Type: "aws:rds/clusterInstance:ClusterInstance", Name: "rds-instance-0", ID: "<PLACEHOLDER>", Parent: "rds"},
		},
	}

	resourceMappings := map[string]string{
		`module.rds.aws_rds_cluster_instance.instance["writer"]`: "rds-instance-0",
	}

	result := FillImportFile(digest, importFile, nil, resourceMappings)

	assert.Equal(t, 1, result.Filled)
	assert.Equal(t, 0, result.Unmatched)
	assert.Equal(t, "rds-writer-instance", importFile.Resources[1].ID)
}

func TestExtractTypeFromURN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		urn      string
		expected string
	}{
		{"urn:pulumi:dev::proj::aws:ec2/vpc:Vpc::main", "aws:ec2/vpc:Vpc"},
		{"urn:pulumi:stack::project::aws:rds/cluster:Cluster::name", "aws:rds/cluster:Cluster"},
		{"module.vpc.aws_vpc.main", ""},
		{"", ""},
		{"urn:pulumi:stack::project", ""},
	}

	for _, tt := range tests {
		t.Run(tt.urn, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractTypeFromURN(tt.urn))
		})
	}
}

func TestExtractResourceName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		address  string
		expected string
	}{
		{"module.vpc.aws_vpc.main", "main"},
		{"module.vpc.aws_subnet.public[0]", "public[0]"},
		{`module.vpc.aws_ssm_parameter.params["my_key"]`, `params["my_key"]`},
		{"aws_s3_bucket.my_bucket", "my_bucket"},
		{`module.capture_ui["dmvhm"].aws_s3_bucket.ui`, "ui"},
	}

	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractResourceName(tt.address))
		})
	}
}

func TestExtractImportSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		parent   string
		expected string
	}{
		{"vpc-main", "vpc", "main"},
		{"rds-aurora_cluster", "rds", "aurora_cluster"},
		{`capture_ui["dmvhm"]-ui`, `capture_ui["dmvhm"]`, "ui"},
		{"my_bucket", "", "my_bucket"},
		// Name doesn't have parent prefix — return as-is
		{"unrelated-name", "vpc", "unrelated-name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractImportSuffix(tt.name, tt.parent))
		})
	}
}

func TestNormalizeInstanceKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"main", "main"},
		{"public[0]", "public_0"},
		{`params["my_key"]`, "params_my_key"},
		{"instances[1]", "instances_1"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeInstanceKey(tt.input))
		})
	}
}

func TestTranslateImportIDs(t *testing.T) {
	t.Parallel()

	digest := &ModuleMap{
		RootResources: []ModuleResource{
			{
				Mode:             "managed",
				ImportID:         "aclid-123",
				TerraformAddress: "aws_wafv2_web_acl.my_acl",
				Attributes: map[string]interface{}{
					"name":  "my-waf-acl",
					"scope": "REGIONAL",
				},
			},
			{
				Mode:             "managed",
				ImportID:         "s3-key-abc",
				TerraformAddress: "aws_s3_object.my_obj",
				Attributes: map[string]interface{}{
					"bucket": "my-bucket",
					"key":    "path/to/file.txt",
				},
			},
			{
				Mode:             "managed",
				ImportID:         "rtbassoc-123",
				TerraformAddress: "aws_route_table_association.public",
				Attributes: map[string]interface{}{
					"subnet_id":      "subnet-abc",
					"route_table_id": "rtb-xyz",
				},
			},
			{
				Mode:             "managed",
				ImportID:         "arn:aws:ecs:us-east-1:123:service/my-cluster/my-svc",
				TerraformAddress: "aws_ecs_service.my_svc",
				Attributes: map[string]interface{}{
					"cluster": "arn:aws:ecs:us-east-1:123:cluster/my-cluster",
					"name":    "my-svc",
				},
			},
			{
				Mode:             "managed",
				ImportID:         "my-fn-id",
				TerraformAddress: "aws_lambda_permission.invoke",
				Attributes: map[string]interface{}{
					"function_name": "my-function",
					"statement_id":  "AllowInvoke",
				},
			},
			{
				Mode:             "managed",
				ImportID:         "not-translated",
				TerraformAddress: "aws_iam_role.my_role",
				Attributes:       map[string]interface{}{"name": "my-role"},
			},
		},
	}

	importFile := &ImportFile{
		NameTable: map[string]string{"provider": "urn:pulumi:stack::proj::pulumi:providers:aws::default"},
		Resources: []ImportEntry{
			{Type: "aws:wafv2/webAcl:WebAcl", Name: "my-acl", ID: "aclid-123", Provider: "aws-us-east-1"},
			{Type: "aws:s3/bucketObject:BucketObject", Name: "my-obj", ID: "s3-key-abc"},
			{Type: "aws:ec2/routeTableAssociation:RouteTableAssociation", Name: "public", ID: "rtbassoc-123"},
			{Type: "aws:ecs/service:Service", Name: "my-svc", ID: "arn:aws:ecs:us-east-1:123:service/my-cluster/my-svc"},
			{Type: "aws:lambda/permission:Permission", Name: "invoke", ID: "my-fn-id"},
			{Type: "aws:iam/role:Role", Name: "my-role", ID: "not-translated"},
		},
	}

	translated := TranslateImportIDs(importFile, digest)

	assert.Equal(t, 5, translated)

	// WAFv2: uuid -> id/name/scope
	assert.Equal(t, "aclid-123/my-waf-acl/REGIONAL", importFile.Resources[0].ID)

	// S3 BucketObject: key -> s3://bucket/key
	assert.Equal(t, "s3://my-bucket/path/to/file.txt", importFile.Resources[1].ID)

	// RouteTableAssociation: rtbassoc -> subnet/rtb
	assert.Equal(t, "subnet-abc/rtb-xyz", importFile.Resources[2].ID)

	// ECS Service: ARN -> cluster-name/service-name
	assert.Equal(t, "my-cluster/my-svc", importFile.Resources[3].ID)

	// Lambda Permission: id -> function/statement
	assert.Equal(t, "my-function/AllowInvoke", importFile.Resources[4].ID)

	// IAM Role: no translation needed
	assert.Equal(t, "not-translated", importFile.Resources[5].ID)

	// Provider and NameTable preserved
	assert.Equal(t, "aws-us-east-1", importFile.Resources[0].Provider)
	assert.NotNil(t, importFile.NameTable)
	assert.Equal(t, "urn:pulumi:stack::proj::pulumi:providers:aws::default", importFile.NameTable["provider"])
}

func TestProviderAndNameTablePreserved(t *testing.T) {
	t.Parallel()

	digest := &ModuleMap{
		RootResources: []ModuleResource{
			{
				Mode:             "managed",
				ImportID:         "vpc-123",
				TranslatedURN:    "urn:pulumi:stack::proj::aws:ec2/vpc:Vpc::my-vpc",
				TerraformAddress: "aws_vpc.main",
			},
		},
	}

	importFile := &ImportFile{
		NameTable: map[string]string{
			"aws-us-east-1": "urn:pulumi:stack::proj::pulumi:providers:aws::aws-us-east-1",
		},
		Resources: []ImportEntry{
			{Type: "aws:ec2/vpc:Vpc", Name: "my-vpc", ID: "<PLACEHOLDER>", Provider: "aws-us-east-1"},
		},
	}

	resourceMappings := map[string]string{
		"aws_vpc.main": "my-vpc",
	}

	result := FillImportFile(digest, importFile, nil, resourceMappings)
	require.Equal(t, 1, result.Filled)

	// ID was filled
	assert.Equal(t, "vpc-123", importFile.Resources[0].ID)

	// Provider preserved through fill
	assert.Equal(t, "aws-us-east-1", importFile.Resources[0].Provider)

	// NameTable preserved
	assert.Equal(t, "urn:pulumi:stack::proj::pulumi:providers:aws::aws-us-east-1",
		importFile.NameTable["aws-us-east-1"])
}
