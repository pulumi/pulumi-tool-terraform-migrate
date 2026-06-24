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
	"context"
	"testing"

	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/providermap"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/tofu"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadAWSProvider loads the AWS provider from the bucket_state.json fixture.
// This reuses the pre-warmed provider cache from TestMain.
func loadAWSProvider(t *testing.T) map[providermap.TerraformProviderName]*ProviderWithMetadata {
	t.Helper()
	ctx := context.Background()
	tfState, err := tofu.LoadTerraformState(ctx, tofu.LoadTerraformStateOptions{
		StateFilePath: "testdata/bucket_state.json",
	})
	require.NoError(t, err)
	providers, err := GetPulumiProvidersForTerraformState(tfState, nil)
	require.NoError(t, err)
	require.NotEmpty(t, providers)
	return providers
}

func TestBuildPulumiToTFTypeMap(t *testing.T) {
	t.Parallel()
	providers := loadAWSProvider(t)
	typeMap := BuildPulumiToTFTypeMap(providers)

	// Verify known AWS type token mappings.
	assert.Equal(t, "aws_s3_bucket", typeMap["aws:s3/bucket:Bucket"])
	assert.Equal(t, "aws_lambda_function", typeMap["aws:lambda/function:Function"])
	assert.Equal(t, "aws_db_instance", typeMap["aws:rds/instance:Instance"])
	assert.Equal(t, "aws_iam_role", typeMap["aws:iam/role:Role"])

	// Map should contain many entries.
	assert.Greater(t, len(typeMap), 100)
}

func TestGetSchemaFieldInfo_S3Bucket(t *testing.T) {
	t.Parallel()
	providers := loadAWSProvider(t)

	// Find the AWS provider.
	var awsProv *ProviderWithMetadata
	for _, prov := range providers {
		if prov.Name == "aws" {
			awsProv = prov
			break
		}
	}
	require.NotNil(t, awsProv, "AWS provider not found")

	fields := GetSchemaFieldInfo(awsProv, "aws_s3_bucket")
	require.NotNil(t, fields)
	require.NotEmpty(t, fields)

	// force_destroy is an optional input field.
	fd, ok := fields["force_destroy"]
	require.True(t, ok, "force_destroy field not found")
	assert.Equal(t, "forceDestroy", fd.PulumiName)
	assert.True(t, fd.IsInput)
	assert.False(t, fd.IsComputed)

	// bucket is optional+computed (not purely computed, so IsInput=true).
	bucket, ok := fields["bucket"]
	require.True(t, ok, "bucket field not found")
	assert.True(t, bucket.IsInput)
	assert.False(t, bucket.IsComputed, "bucket is optional+computed, not purely computed")

	// arn should be computed-only.
	arn, ok := fields["arn"]
	require.True(t, ok, "arn field not found")
	assert.True(t, arn.IsComputed)
	assert.False(t, arn.IsInput)
}

func TestGetSchemaFieldInfo_LambdaFunction(t *testing.T) {
	t.Parallel()
	providers := loadAWSProvider(t)

	var awsProv *ProviderWithMetadata
	for _, prov := range providers {
		if prov.Name == "aws" {
			awsProv = prov
			break
		}
	}
	require.NotNil(t, awsProv, "AWS provider not found")

	fields := GetSchemaFieldInfo(awsProv, "aws_lambda_function")
	require.NotNil(t, fields)

	// filename is an asset field (FileAsset with code as its Pulumi name).
	fn, ok := fields["filename"]
	require.True(t, ok, "filename field not found")
	assert.True(t, fn.IsAsset)
	assert.True(t, fn.IsInput)
	assert.NotEmpty(t, fn.HashField, "expected a hash field for the filename asset")
}

func TestGetSchemaFieldInfo_NonexistentResource(t *testing.T) {
	t.Parallel()
	providers := loadAWSProvider(t)

	var awsProv *ProviderWithMetadata
	for _, prov := range providers {
		if prov.Name == "aws" {
			awsProv = prov
			break
		}
	}
	require.NotNil(t, awsProv)

	fields := GetSchemaFieldInfo(awsProv, "aws_nonexistent_resource")
	assert.Nil(t, fields)
}

func TestLookupProviderForPulumiType(t *testing.T) {
	t.Parallel()
	providers := loadAWSProvider(t)
	typeMap := BuildPulumiToTFTypeMap(providers)

	// Successful lookup.
	prov, tfType, ok := LookupProviderForPulumiType("aws:s3/bucket:Bucket", typeMap, providers)
	assert.True(t, ok)
	assert.Equal(t, "aws_s3_bucket", tfType)
	assert.NotNil(t, prov)
	assert.Equal(t, "aws", prov.Name)

	// Unknown type token.
	_, _, ok = LookupProviderForPulumiType("unknown:type:Token", typeMap, providers)
	assert.False(t, ok)
}
