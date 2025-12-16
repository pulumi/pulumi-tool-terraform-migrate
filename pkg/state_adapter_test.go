package pkg

import (
	"context"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hexops/autogold/v2"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/urn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertSimple(t *testing.T) {
	data, err := TranslateStateWithWorkspace("testdata/bucket_state.json", setupMinimalPulumiTestProject(t))
	if err != nil {
		t.Fatalf("failed to convert Terraform state: %v", err)
	}

	autogold.Expect(int(3)).Equal(t, len(data.Deployment.Resources))

	awsProv := findByURN("urn:pulumi:dev::example::pulumi:providers:aws::default_7_12_0",
		data.Deployment.Resources)

	autogold.Expect(map[string]interface{}{
		"region": "us-east-1", "skipCredentialsValidation": false,
		"skipRegionValidation": true,
		"version":              "7.12.0",
	}).Equal(t, awsProv.Inputs)

	autogold.Expect(map[string]interface{}{
		"region": "us-east-1", "skipCredentialsValidation": false,
		"skipRegionValidation": true,
		"version":              "7.12.0",
	}).Equal(t, awsProv.Outputs)

	bucket := findByURN("urn:pulumi:dev::example::aws:s3/bucket:Bucket::example",
		data.Deployment.Resources)

	autogold.Expect(map[string]interface{}{
		"__defaults": []interface{}{}, "bucket": "my-example-bucket-20251119163156", "grants": []interface{}{map[string]interface{}{
			"__defaults":  []interface{}{},
			"id":          "69c80caa37c265d93308334e41ddd9ee253ab9f1460124a3c64fa7e21f3ef5b3",
			"permissions": []interface{}{"FULL_CONTROL"},
			"type":        "CanonicalUser",
		}},
		"region":       "us-east-1",
		"requestPayer": "BucketOwner",
		"serverSideEncryptionConfiguration": map[string]interface{}{
			"__defaults": []interface{}{},
			"rule": map[string]interface{}{
				"__defaults": []interface{}{},
				"applyServerSideEncryptionByDefault": map[string]interface{}{
					"__defaults":   []interface{}{},
					"sseAlgorithm": "AES256",
				},
			},
		},
	}).Equal(t, bucket.Inputs)

	autogold.Expect(map[string]interface{}{
		"accelerationStatus": "", "acl": nil, "arn": "arn:aws:s3:::my-example-bucket-20251119163156",
		"bucket":                   "my-example-bucket-20251119163156",
		"bucketDomainName":         "my-example-bucket-20251119163156.s3.amazonaws.com",
		"bucketPrefix":             "",
		"bucketRegion":             "us-east-1",
		"bucketRegionalDomainName": "my-example-bucket-20251119163156.s3.us-east-1.amazonaws.com",
		"corsRules":                []interface{}{},
		"forceDestroy":             false,
		"grants": []interface{}{map[string]interface{}{
			"id":          "69c80caa37c265d93308334e41ddd9ee253ab9f1460124a3c64fa7e21f3ef5b3",
			"permissions": []interface{}{"FULL_CONTROL"},
			"type":        "CanonicalUser",
			"uri":         "",
		}},
		"hostedZoneId":             "Z3AQBSTGFYJSTF",
		"id":                       "my-example-bucket-20251119163156",
		"lifecycleRules":           []interface{}{},
		"logging":                  nil,
		"objectLockConfiguration":  nil,
		"objectLockEnabled":        false,
		"policy":                   "",
		"region":                   "us-east-1",
		"replicationConfiguration": nil,
		"requestPayer":             "BucketOwner",
		"serverSideEncryptionConfiguration": map[string]interface{}{"rule": map[string]interface{}{
			"applyServerSideEncryptionByDefault": map[string]interface{}{
				"kmsMasterKeyId": "",
				"sseAlgorithm":   "AES256",
			},
			"bucketKeyEnabled": false,
		}},
		"tags":    nil,
		"tagsAll": map[string]interface{}{},
		"versioning": map[string]interface{}{
			"enabled":   false,
			"mfaDelete": false,
		},
		"website":         nil,
		"websiteDomain":   nil,
		"websiteEndpoint": nil,
	}).Equal(t, bucket.Outputs)
}

func TestConvertInvolved(t *testing.T) {
	data, err := TranslateStateWithWorkspace("testdata/tofu_state.json", setupMinimalPulumiTestProject(t))
	if err != nil {
		t.Fatalf("failed to convert Terraform state: %v", err)
	}

	type testCase struct {
		resourceURN   urn.URN
		expectInputs  autogold.Value
		expectOutputs autogold.Value
	}

	testCases := []testCase{
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::pulumi:providers:aws::default_7_12_0"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"region": "us-east-1", "skipCredentialsValidation": false,
				"skipRegionValidation": true,
				"version":              "7.12.0",
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"region": "us-east-1", "skipCredentialsValidation": false,
				"skipRegionValidation": true,
				"version":              "7.12.0",
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:cloudwatch/logGroup:LogGroup::lambda_logs"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "logGroupClass": "STANDARD", "name": "/aws/lambda/data-pipeline-data-processor",
				"region":          "us-east-1",
				"retentionInDays": 14,
				"tags": map[string]interface{}{
					"Purpose":    "Lambda Logs",
					"__defaults": []interface{}{},
				},
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
					"Purpose":     "Lambda Logs",
					"__defaults":  []interface{}{},
				},
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"arn":             "arn:aws:logs:us-east-1:894850187425:log-group:/aws/lambda/data-pipeline-data-processor",
				"id":              "/aws/lambda/data-pipeline-data-processor",
				"kmsKeyId":        "",
				"logGroupClass":   "STANDARD",
				"name":            "/aws/lambda/data-pipeline-data-processor",
				"namePrefix":      "",
				"region":          "us-east-1",
				"retentionInDays": 14,
				"skipDestroy":     false,
				"tags":            map[string]interface{}{"Purpose": "Lambda Logs"},
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
					"Purpose":     "Lambda Logs",
				},
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:dynamodb/table:Table::file_metadata"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "attributes": []interface{}{
					map[string]interface{}{"__defaults": []interface{}{}, "name": "file_id", "type": "S"},
					map[string]interface{}{
						"__defaults": []interface{}{},
						"name":       "status",
						"type":       "S",
					},
					map[string]interface{}{
						"__defaults": []interface{}{},
						"name":       "timestamp",
						"type":       "N",
					},
				},
				"billingMode": "PAY_PER_REQUEST",
				"globalSecondaryIndexes": []interface{}{map[string]interface{}{
					"__defaults":     []interface{}{},
					"hashKey":        "status",
					"name":           "status-index",
					"projectionType": "ALL",
					"rangeKey":       "timestamp",
				}},
				"hashKey": "file_id",
				"name":    "data-pipeline-file-metadata",
				"pointInTimeRecovery": map[string]interface{}{
					"__defaults":           []interface{}{},
					"enabled":              true,
					"recoveryPeriodInDays": 35,
				},
				"rangeKey":   "timestamp",
				"region":     "us-east-1",
				"tableClass": "STANDARD",
				"tags": map[string]interface{}{
					"Purpose":    "File Metadata Tracking",
					"__defaults": []interface{}{},
				},
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
					"Purpose":     "File Metadata Tracking",
					"__defaults":  []interface{}{},
				},
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"arn": "arn:aws:dynamodb:us-east-1:894850187425:table/data-pipeline-file-metadata",
				"attributes": []interface{}{
					map[string]interface{}{
						"name": "file_id",
						"type": "S",
					},
					map[string]interface{}{
						"name": "status",
						"type": "S",
					},
					map[string]interface{}{
						"name": "timestamp",
						"type": "N",
					},
				},
				"billingMode":               "PAY_PER_REQUEST",
				"deletionProtectionEnabled": false,
				"globalSecondaryIndexes": []interface{}{map[string]interface{}{
					"hashKey":            "status",
					"name":               "status-index",
					"nonKeyAttributes":   []interface{}{},
					"onDemandThroughput": nil,
					"projectionType":     "ALL",
					"rangeKey":           "timestamp",
					"readCapacity":       0,
					"warmThroughput":     nil,
					"writeCapacity":      0,
				}},
				"hashKey":               "file_id",
				"id":                    "data-pipeline-file-metadata",
				"importTable":           nil,
				"localSecondaryIndexes": []interface{}{},
				"name":                  "data-pipeline-file-metadata",
				"onDemandThroughput":    nil,
				"pointInTimeRecovery": map[string]interface{}{
					"enabled":              true,
					"recoveryPeriodInDays": 35,
				},
				"rangeKey":              "timestamp",
				"readCapacity":          0,
				"region":                "us-east-1",
				"replicas":              []interface{}{},
				"restoreDateTime":       nil,
				"restoreSourceName":     nil,
				"restoreSourceTableArn": nil,
				"restoreToLatestTime":   nil,
				"serverSideEncryption":  nil,
				"streamArn":             "",
				"streamEnabled":         false,
				"streamLabel":           "",
				"streamViewType":        "",
				"tableClass":            "STANDARD",
				"tags":                  map[string]interface{}{"Purpose": "File Metadata Tracking"},
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
					"Purpose":     "File Metadata Tracking",
				},
				"ttl": map[string]interface{}{
					"attributeName": "",
					"enabled":       false,
				},
				"warmThroughput": nil,
				"writeCapacity":  0,
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:iam/role:Role::lambda_role"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "assumeRolePolicy": `{"Statement":[{"Action":"sts:AssumeRole","Effect":"Allow","Principal":{"Service":"lambda.amazonaws.com"}}],"Version":"2012-10-17"}`,
				"inlinePolicies": []interface{}{map[string]interface{}{
					"__defaults": []interface{}{},
					"name":       "data-pipeline-lambda-s3-access",
					"policy":     `{"Version":"2012-10-17","Statement":[{"Action":["s3:GetObject","s3:PutObject","s3:DeleteObject","s3:ListBucket"],"Effect":"Allow","Resource":["arn:aws:s3:::data-pipeline-data-lake-894850187425","arn:aws:s3:::data-pipeline-data-lake-894850187425/*"]},{"Action":["sqs:ReceiveMessage","sqs:DeleteMessage","sqs:GetQueueAttributes"],"Effect":"Allow","Resource":"arn:aws:sqs:us-east-1:894850187425:data-pipeline-data-processing"},{"Action":["logs:CreateLogGroup","logs:CreateLogStream","logs:PutLogEvents"],"Effect":"Allow","Resource":"arn:aws:logs:us-east-1:894850187425:*"}]}`,
				}},
				"maxSessionDuration": 3600,
				"name":               "data-pipeline-lambda-role",
				"path":               "/",
				"tags": map[string]interface{}{
					"Purpose":    "Lambda Execution Role",
					"__defaults": []interface{}{},
				},
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
					"Purpose":     "Lambda Execution Role",
					"__defaults":  []interface{}{},
				},
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"arn":                 "arn:aws:iam::894850187425:role/data-pipeline-lambda-role",
				"assumeRolePolicy":    `{"Statement":[{"Action":"sts:AssumeRole","Effect":"Allow","Principal":{"Service":"lambda.amazonaws.com"}}],"Version":"2012-10-17"}`,
				"createDate":          "2025-12-03T16:01:07Z",
				"description":         "",
				"forceDetachPolicies": false,
				"id":                  "data-pipeline-lambda-role",
				"inlinePolicies": []interface{}{map[string]interface{}{
					"name":   "data-pipeline-lambda-s3-access",
					"policy": `{"Version":"2012-10-17","Statement":[{"Action":["s3:GetObject","s3:PutObject","s3:DeleteObject","s3:ListBucket"],"Effect":"Allow","Resource":["arn:aws:s3:::data-pipeline-data-lake-894850187425","arn:aws:s3:::data-pipeline-data-lake-894850187425/*"]},{"Action":["sqs:ReceiveMessage","sqs:DeleteMessage","sqs:GetQueueAttributes"],"Effect":"Allow","Resource":"arn:aws:sqs:us-east-1:894850187425:data-pipeline-data-processing"},{"Action":["logs:CreateLogGroup","logs:CreateLogStream","logs:PutLogEvents"],"Effect":"Allow","Resource":"arn:aws:logs:us-east-1:894850187425:*"}]}`,
				}},
				"managedPolicyArns":   []interface{}{},
				"maxSessionDuration":  3600,
				"name":                "data-pipeline-lambda-role",
				"namePrefix":          "",
				"path":                "/",
				"permissionsBoundary": "",
				"tags":                map[string]interface{}{"Purpose": "Lambda Execution Role"},
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
					"Purpose":     "Lambda Execution Role",
				},
				"uniqueId": "AROA5AWJ2ICQ3X6RQS3YE",
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:iam/rolePolicy:RolePolicy::lambda_s3_access"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "name": "data-pipeline-lambda-s3-access", "policy": `{"Version":"2012-10-17","Statement":[{"Action":["s3:GetObject","s3:PutObject","s3:DeleteObject","s3:ListBucket"],"Effect":"Allow","Resource":["arn:aws:s3:::data-pipeline-data-lake-894850187425","arn:aws:s3:::data-pipeline-data-lake-894850187425/*"]},{"Action":["sqs:ReceiveMessage","sqs:DeleteMessage","sqs:GetQueueAttributes"],"Effect":"Allow","Resource":"arn:aws:sqs:us-east-1:894850187425:data-pipeline-data-processing"},{"Action":["logs:CreateLogGroup","logs:CreateLogStream","logs:PutLogEvents"],"Effect":"Allow","Resource":"arn:aws:logs:us-east-1:894850187425:*"}]}`,
				"role": "data-pipeline-lambda-role",
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"id":         "data-pipeline-lambda-role:data-pipeline-lambda-s3-access",
				"name":       "data-pipeline-lambda-s3-access",
				"namePrefix": "",
				"policy":     `{"Version":"2012-10-17","Statement":[{"Action":["s3:GetObject","s3:PutObject","s3:DeleteObject","s3:ListBucket"],"Effect":"Allow","Resource":["arn:aws:s3:::data-pipeline-data-lake-894850187425","arn:aws:s3:::data-pipeline-data-lake-894850187425/*"]},{"Action":["sqs:ReceiveMessage","sqs:DeleteMessage","sqs:GetQueueAttributes"],"Effect":"Allow","Resource":"arn:aws:sqs:us-east-1:894850187425:data-pipeline-data-processing"},{"Action":["logs:CreateLogGroup","logs:CreateLogStream","logs:PutLogEvents"],"Effect":"Allow","Resource":"arn:aws:logs:us-east-1:894850187425:*"}]}`,
				"role":       "data-pipeline-lambda-role",
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:lambda/eventSourceMapping:EventSourceMapping::sqs_trigger"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "batchSize": 10, "enabled": true, "eventSourceArn": "arn:aws:sqs:us-east-1:894850187425:data-pipeline-data-processing",
				"functionName": "arn:aws:lambda:us-east-1:894850187425:function:data-pipeline-data-processor",
				"region":       "us-east-1",
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
					"__defaults":  []interface{}{},
				},
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"amazonManagedKafkaEventSourceConfig": nil, "arn": "arn:aws:lambda:us-east-1:894850187425:event-source-mapping:2d7e0b59-c5b5-4654-a2ae-d391fd522e98",
				"batchSize":                         10,
				"bisectBatchOnFunctionError":        false,
				"destinationConfig":                 nil,
				"documentDbEventSourceConfig":       nil,
				"enabled":                           true,
				"eventSourceArn":                    "arn:aws:sqs:us-east-1:894850187425:data-pipeline-data-processing",
				"filterCriteria":                    nil,
				"functionArn":                       "arn:aws:lambda:us-east-1:894850187425:function:data-pipeline-data-processor",
				"functionName":                      "arn:aws:lambda:us-east-1:894850187425:function:data-pipeline-data-processor",
				"functionResponseTypes":             []interface{}{},
				"id":                                "2d7e0b59-c5b5-4654-a2ae-d391fd522e98",
				"kmsKeyArn":                         "",
				"lastModified":                      "2025-12-03T16:02:16Z",
				"lastProcessingResult":              "",
				"maximumBatchingWindowInSeconds":    0,
				"maximumRecordAgeInSeconds":         0,
				"maximumRetryAttempts":              0,
				"metricsConfig":                     nil,
				"parallelizationFactor":             0,
				"provisionedPollerConfig":           nil,
				"queues":                            nil,
				"region":                            "us-east-1",
				"scalingConfig":                     nil,
				"selfManagedEventSource":            nil,
				"selfManagedKafkaEventSourceConfig": nil,
				"sourceAccessConfigurations":        []interface{}{},
				"startingPosition":                  "",
				"startingPositionTimestamp":         "",
				"state":                             "Enabled",
				"stateTransitionReason":             "USER_INITIATED",
				"tags":                              map[string]interface{}{},
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
				},
				"topics":                  []interface{}{},
				"tumblingWindowInSeconds": 0,
				"uuid":                    "2d7e0b59-c5b5-4654-a2ae-d391fd522e98",
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:lambda/function:Function::data_processor"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "architectures": []interface{}{"x86_64"}, "code": "./lambda_function.zip", "environment": map[string]interface{}{
					"__defaults": []interface{}{},
					"variables": map[string]interface{}{
						"DATA_LAKE_BUCKET": "data-pipeline-data-lake-894850187425",
						"ENVIRONMENT":      "dev",
						"__defaults":       []interface{}{},
					},
				},
				"ephemeralStorage": map[string]interface{}{
					"__defaults": []interface{}{},
					"size":       512,
				},
				"handler": "lambda_function.handler",
				"loggingConfig": map[string]interface{}{
					"__defaults": []interface{}{},
					"logFormat":  "Text",
					"logGroup":   "/aws/lambda/data-pipeline-data-processor",
				},
				"memorySize":                   256,
				"name":                         "data-pipeline-data-processor",
				"packageType":                  "Zip",
				"region":                       "us-east-1",
				"reservedConcurrentExecutions": -1,
				"role":                         "arn:aws:iam::894850187425:role/data-pipeline-lambda-role",
				"runtime":                      "python3.11",
				"sourceCodeHash":               "Uk59ixjEaDAJDlKLgE/2RRsCDSterPmhbfoI1psRc90=",
				"tags": map[string]interface{}{
					"Purpose":    "Data Processing",
					"__defaults": []interface{}{},
				},
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
					"Purpose":     "Data Processing",
					"__defaults":  []interface{}{},
				},
				"timeout": 60,
				"tracingConfig": map[string]interface{}{
					"__defaults": []interface{}{},
					"mode":       "PassThrough",
				},
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"architectures": []interface{}{"x86_64"}, "arn": "arn:aws:lambda:us-east-1:894850187425:function:data-pipeline-data-processor",
				"code":                 "./lambda_function.zip",
				"codeSha256":           "Uk59ixjEaDAJDlKLgE/2RRsCDSterPmhbfoI1psRc90=",
				"codeSigningConfigArn": "",
				"deadLetterConfig":     nil,
				"description":          "",
				"environment": map[string]interface{}{"variables": map[string]interface{}{
					"DATA_LAKE_BUCKET": "data-pipeline-data-lake-894850187425",
					"ENVIRONMENT":      "dev",
				}},
				"ephemeralStorage": map[string]interface{}{"size": 512},
				"fileSystemConfig": nil,
				"handler":          "lambda_function.handler",
				"id":               "data-pipeline-data-processor",
				"imageConfig":      nil,
				"imageUri":         "",
				"invokeArn":        "arn:aws:apigateway:us-east-1:lambda:path/2015-03-31/functions/arn:aws:lambda:us-east-1:894850187425:function:data-pipeline-data-processor/invocations",
				"kmsKeyArn":        "",
				"lastModified":     "2025-12-03T16:01:19.572+0000",
				"layers":           []interface{}{},
				"loggingConfig": map[string]interface{}{
					"applicationLogLevel": "",
					"logFormat":           "Text",
					"logGroup":            "/aws/lambda/data-pipeline-data-processor",
					"systemLogLevel":      "",
				},
				"memorySize":                     256,
				"name":                           "data-pipeline-data-processor",
				"packageType":                    "Zip",
				"publish":                        false,
				"qualifiedArn":                   "arn:aws:lambda:us-east-1:894850187425:function:data-pipeline-data-processor:$LATEST",
				"qualifiedInvokeArn":             "arn:aws:apigateway:us-east-1:lambda:path/2015-03-31/functions/arn:aws:lambda:us-east-1:894850187425:function:data-pipeline-data-processor:$LATEST/invocations",
				"region":                         "us-east-1",
				"replaceSecurityGroupsOnDestroy": nil,
				"replacementSecurityGroupIds":    nil,
				"reservedConcurrentExecutions":   -1,
				"role":                           "arn:aws:iam::894850187425:role/data-pipeline-lambda-role",
				"runtime":                        "python3.11",
				"s3Bucket":                       nil,
				"s3Key":                          nil,
				"s3ObjectVersion":                nil,
				"signingJobArn":                  "",
				"signingProfileVersionArn":       "",
				"skipDestroy":                    false,
				"snapStart":                      nil,
				"sourceCodeHash":                 "Uk59ixjEaDAJDlKLgE/2RRsCDSterPmhbfoI1psRc90=",
				"sourceCodeSize":                 459,
				"sourceKmsKeyArn":                "",
				"tags":                           map[string]interface{}{"Purpose": "Data Processing"},
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
					"Purpose":     "Data Processing",
				},
				"timeout":       60,
				"tracingConfig": map[string]interface{}{"mode": "PassThrough"},
				"version":       "$LATEST",
				"vpcConfig":     nil,
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:s3/bucket:Bucket::lambda_artifacts"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "bucket": "data-pipeline-lambda-artifacts-894850187425",
				"grants": []interface{}{map[string]interface{}{
					"__defaults":  []interface{}{},
					"id":          "69c80caa37c265d93308334e41ddd9ee253ab9f1460124a3c64fa7e21f3ef5b3",
					"permissions": []interface{}{"FULL_CONTROL"},
					"type":        "CanonicalUser",
				}},
				"region":       "us-east-1",
				"requestPayer": "BucketOwner",
				"serverSideEncryptionConfiguration": map[string]interface{}{
					"__defaults": []interface{}{},
					"rule": map[string]interface{}{
						"__defaults": []interface{}{},
						"applyServerSideEncryptionByDefault": map[string]interface{}{
							"__defaults":   []interface{}{},
							"sseAlgorithm": "AES256",
						},
					},
				},
				"tags": map[string]interface{}{
					"Purpose":    "Lambda Deployment Artifacts",
					"__defaults": []interface{}{},
				},
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
					"Purpose":     "Lambda Deployment Artifacts",
					"__defaults":  []interface{}{},
				},
				"versioning": map[string]interface{}{
					"__defaults": []interface{}{},
					"enabled":    true,
				},
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"accelerationStatus": "", "acl": nil, "arn": "arn:aws:s3:::data-pipeline-lambda-artifacts-894850187425",
				"bucket":                   "data-pipeline-lambda-artifacts-894850187425",
				"bucketDomainName":         "data-pipeline-lambda-artifacts-894850187425.s3.amazonaws.com",
				"bucketPrefix":             "",
				"bucketRegion":             "us-east-1",
				"bucketRegionalDomainName": "data-pipeline-lambda-artifacts-894850187425.s3.us-east-1.amazonaws.com",
				"corsRules":                []interface{}{},
				"forceDestroy":             false,
				"grants": []interface{}{map[string]interface{}{
					"id":          "69c80caa37c265d93308334e41ddd9ee253ab9f1460124a3c64fa7e21f3ef5b3",
					"permissions": []interface{}{"FULL_CONTROL"},
					"type":        "CanonicalUser",
					"uri":         "",
				}},
				"hostedZoneId":             "Z3AQBSTGFYJSTF",
				"id":                       "data-pipeline-lambda-artifacts-894850187425",
				"lifecycleRules":           []interface{}{},
				"logging":                  nil,
				"objectLockConfiguration":  nil,
				"objectLockEnabled":        false,
				"policy":                   "",
				"region":                   "us-east-1",
				"replicationConfiguration": nil,
				"requestPayer":             "BucketOwner",
				"serverSideEncryptionConfiguration": map[string]interface{}{"rule": map[string]interface{}{
					"applyServerSideEncryptionByDefault": map[string]interface{}{
						"kmsMasterKeyId": "",
						"sseAlgorithm":   "AES256",
					},
					"bucketKeyEnabled": false,
				}},
				"tags": map[string]interface{}{"Purpose": "Lambda Deployment Artifacts"},
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
					"Purpose":     "Lambda Deployment Artifacts",
				},
				"versioning": map[string]interface{}{
					"enabled":   true,
					"mfaDelete": false,
				},
				"website":         nil,
				"websiteDomain":   nil,
				"websiteEndpoint": nil,
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:s3/bucketPublicAccessBlock:BucketPublicAccessBlock::lambda_artifacts"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "blockPublicAcls": true, "blockPublicPolicy": true,
				"bucket":                "data-pipeline-lambda-artifacts-894850187425",
				"ignorePublicAcls":      true,
				"region":                "us-east-1",
				"restrictPublicBuckets": true,
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"blockPublicAcls": true, "blockPublicPolicy": true,
				"bucket":                "data-pipeline-lambda-artifacts-894850187425",
				"id":                    "data-pipeline-lambda-artifacts-894850187425",
				"ignorePublicAcls":      true,
				"region":                "us-east-1",
				"restrictPublicBuckets": true,
				"skipDestroy":           nil,
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:s3/bucketServerSideEncryptionConfiguration:BucketServerSideEncryptionConfiguration::lambda_artifacts"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "bucket": "data-pipeline-lambda-artifacts-894850187425",
				"region": "us-east-1",
				"rules": []interface{}{map[string]interface{}{
					"__defaults": []interface{}{},
					"applyServerSideEncryptionByDefault": map[string]interface{}{
						"__defaults":   []interface{}{},
						"sseAlgorithm": "AES256",
					},
				}},
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"bucket":              "data-pipeline-lambda-artifacts-894850187425",
				"expectedBucketOwner": "",
				"id":                  "data-pipeline-lambda-artifacts-894850187425",
				"region":              "us-east-1",
				"rules": []interface{}{map[string]interface{}{
					"applyServerSideEncryptionByDefault": map[string]interface{}{
						"kmsMasterKeyId": "",
						"sseAlgorithm":   "AES256",
					},
					"bucketKeyEnabled": false,
				}},
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:s3/bucketVersioning:BucketVersioning::lambda_artifacts"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "bucket": "data-pipeline-lambda-artifacts-894850187425",
				"region": "us-east-1",
				"versioningConfiguration": map[string]interface{}{
					"__defaults": []interface{}{},
					"status":     "Enabled",
				},
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"bucket":              "data-pipeline-lambda-artifacts-894850187425",
				"expectedBucketOwner": "",
				"id":                  "data-pipeline-lambda-artifacts-894850187425",
				"mfa":                 nil,
				"region":              "us-east-1",
				"versioningConfiguration": map[string]interface{}{
					"mfaDelete": "",
					"status":    "Enabled",
				},
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:sns/topic:Topic::s3_notifications"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "name": "data-pipeline-s3-notifications", "policy": `{"Statement":[{"Action":"sns:Publish","Condition":{"ArnLike":{"aws:SourceArn":"arn:aws:s3:::data-pipeline-data-lake-894850187425"}},"Effect":"Allow","Principal":{"Service":"s3.amazonaws.com"},"Resource":"arn:aws:sns:us-east-1:894850187425:data-pipeline-s3-notifications","Sid":"AllowS3Publish"}],"Version":"2012-10-17"}`,
				"region": "us-east-1",
				"tags": map[string]interface{}{
					"Purpose":    "S3 Event Notifications",
					"__defaults": []interface{}{},
				},
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
					"Purpose":     "S3 Event Notifications",
					"__defaults":  []interface{}{},
				},
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"applicationFailureFeedbackRoleArn": "", "applicationSuccessFeedbackRoleArn": "",
				"applicationSuccessFeedbackSampleRate": 0,
				"archivePolicy":                        "",
				"arn":                                  "arn:aws:sns:us-east-1:894850187425:data-pipeline-s3-notifications",
				"beginningArchiveTime":                 "",
				"contentBasedDeduplication":            false,
				"deliveryPolicy":                       "",
				"displayName":                          "",
				"fifoThroughputScope":                  "",
				"fifoTopic":                            false,
				"firehoseFailureFeedbackRoleArn":       "",
				"firehoseSuccessFeedbackRoleArn":       "",
				"firehoseSuccessFeedbackSampleRate":    0,
				"httpFailureFeedbackRoleArn":           "",
				"httpSuccessFeedbackRoleArn":           "",
				"httpSuccessFeedbackSampleRate":        0,
				"id":                                   "arn:aws:sns:us-east-1:894850187425:data-pipeline-s3-notifications",
				"kmsMasterKeyId":                       "",
				"lambdaFailureFeedbackRoleArn":         "",
				"lambdaSuccessFeedbackRoleArn":         "",
				"lambdaSuccessFeedbackSampleRate":      0,
				"name":                                 "data-pipeline-s3-notifications",
				"namePrefix":                           "",
				"owner":                                "894850187425",
				"policy":                               `{"Statement":[{"Action":"sns:Publish","Condition":{"ArnLike":{"aws:SourceArn":"arn:aws:s3:::data-pipeline-data-lake-894850187425"}},"Effect":"Allow","Principal":{"Service":"s3.amazonaws.com"},"Resource":"arn:aws:sns:us-east-1:894850187425:data-pipeline-s3-notifications","Sid":"AllowS3Publish"}],"Version":"2012-10-17"}`,
				"region":                               "us-east-1",
				"signatureVersion":                     0,
				"sqsFailureFeedbackRoleArn":            "",
				"sqsSuccessFeedbackRoleArn":            "",
				"sqsSuccessFeedbackSampleRate":         0,
				"tags":                                 map[string]interface{}{"Purpose": "S3 Event Notifications"},
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
					"Purpose":     "S3 Event Notifications",
				},
				"tracingConfig": "",
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:sns/topicPolicy:TopicPolicy::s3_notifications"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "arn": "arn:aws:sns:us-east-1:894850187425:data-pipeline-s3-notifications",
				"policy": `{"Statement":[{"Action":"sns:Publish","Condition":{"ArnLike":{"aws:SourceArn":"arn:aws:s3:::data-pipeline-data-lake-894850187425"}},"Effect":"Allow","Principal":{"Service":"s3.amazonaws.com"},"Resource":"arn:aws:sns:us-east-1:894850187425:data-pipeline-s3-notifications","Sid":"AllowS3Publish"}],"Version":"2012-10-17"}`,
				"region": "us-east-1",
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"arn":    "arn:aws:sns:us-east-1:894850187425:data-pipeline-s3-notifications",
				"id":     "arn:aws:sns:us-east-1:894850187425:data-pipeline-s3-notifications",
				"owner":  "894850187425",
				"policy": `{"Statement":[{"Action":"sns:Publish","Condition":{"ArnLike":{"aws:SourceArn":"arn:aws:s3:::data-pipeline-data-lake-894850187425"}},"Effect":"Allow","Principal":{"Service":"s3.amazonaws.com"},"Resource":"arn:aws:sns:us-east-1:894850187425:data-pipeline-s3-notifications","Sid":"AllowS3Publish"}],"Version":"2012-10-17"}`,
				"region": "us-east-1",
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:sns/topicSubscription:TopicSubscription::sqs_subscription"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "confirmationTimeoutInMinutes": 1, "endpoint": "arn:aws:sqs:us-east-1:894850187425:data-pipeline-data-processing",
				"protocol": "sqs",
				"region":   "us-east-1",
				"topic":    "arn:aws:sns:us-east-1:894850187425:data-pipeline-s3-notifications",
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"arn":                          "arn:aws:sns:us-east-1:894850187425:data-pipeline-s3-notifications:d7f108c5-401d-4722-bb9d-eb2669c8d76f",
				"confirmationTimeoutInMinutes": 1,
				"confirmationWasAuthenticated": true,
				"deliveryPolicy":               "",
				"endpoint":                     "arn:aws:sqs:us-east-1:894850187425:data-pipeline-data-processing",
				"endpointAutoConfirms":         false,
				"filterPolicy":                 "",
				"filterPolicyScope":            "",
				"id":                           "arn:aws:sns:us-east-1:894850187425:data-pipeline-s3-notifications:d7f108c5-401d-4722-bb9d-eb2669c8d76f",
				"ownerId":                      "894850187425",
				"pendingConfirmation":          false,
				"protocol":                     "sqs",
				"rawMessageDelivery":           false,
				"redrivePolicy":                "",
				"region":                       "us-east-1",
				"replayPolicy":                 "",
				"subscriptionRoleArn":          "",
				"topic":                        "arn:aws:sns:us-east-1:894850187425:data-pipeline-s3-notifications",
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:sqs/queue:Queue::data_processing"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "kmsDataKeyReusePeriodSeconds": 300, "maxMessageSize": 262144,
				"messageRetentionSeconds": 86400,
				"name":                    "data-pipeline-data-processing",
				"policy":                  `{"Statement":[{"Action":"sqs:SendMessage","Condition":{"ArnEquals":{"aws:SourceArn":"arn:aws:sns:us-east-1:894850187425:data-pipeline-s3-notifications"}},"Effect":"Allow","Principal":{"Service":"sns.amazonaws.com"},"Resource":"arn:aws:sqs:us-east-1:894850187425:data-pipeline-data-processing","Sid":"AllowSNSMessages"}],"Version":"2012-10-17"}`,
				"receiveWaitTimeSeconds":  10,
				"redrivePolicy":           `{"deadLetterTargetArn":"arn:aws:sqs:us-east-1:894850187425:data-pipeline-data-processing-dlq","maxReceiveCount":3}`,
				"region":                  "us-east-1",
				"sqsManagedSseEnabled":    true,
				"tags": map[string]interface{}{
					"Purpose":    "Data Processing Queue",
					"__defaults": []interface{}{},
				},
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
					"Purpose":     "Data Processing Queue",
					"__defaults":  []interface{}{},
				},
				"visibilityTimeoutSeconds": 300,
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"arn":                          "arn:aws:sqs:us-east-1:894850187425:data-pipeline-data-processing",
				"contentBasedDeduplication":    false,
				"deduplicationScope":           "",
				"delaySeconds":                 0,
				"fifoQueue":                    false,
				"fifoThroughputLimit":          "",
				"id":                           "https://sqs.us-east-1.amazonaws.com/894850187425/data-pipeline-data-processing",
				"kmsDataKeyReusePeriodSeconds": 300,
				"kmsMasterKeyId":               "",
				"maxMessageSize":               262144,
				"messageRetentionSeconds":      86400,
				"name":                         "data-pipeline-data-processing",
				"namePrefix":                   "",
				"policy":                       `{"Statement":[{"Action":"sqs:SendMessage","Condition":{"ArnEquals":{"aws:SourceArn":"arn:aws:sns:us-east-1:894850187425:data-pipeline-s3-notifications"}},"Effect":"Allow","Principal":{"Service":"sns.amazonaws.com"},"Resource":"arn:aws:sqs:us-east-1:894850187425:data-pipeline-data-processing","Sid":"AllowSNSMessages"}],"Version":"2012-10-17"}`,
				"receiveWaitTimeSeconds":       10,
				"redriveAllowPolicy":           "",
				"redrivePolicy":                `{"deadLetterTargetArn":"arn:aws:sqs:us-east-1:894850187425:data-pipeline-data-processing-dlq","maxReceiveCount":3}`,
				"region":                       "us-east-1",
				"sqsManagedSseEnabled":         true,
				"tags":                         map[string]interface{}{"Purpose": "Data Processing Queue"},
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
					"Purpose":     "Data Processing Queue",
				},
				"url":                      "https://sqs.us-east-1.amazonaws.com/894850187425/data-pipeline-data-processing",
				"visibilityTimeoutSeconds": 300,
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:sqs/queue:Queue::data_processing_dlq"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "kmsDataKeyReusePeriodSeconds": 300, "maxMessageSize": 262144,
				"messageRetentionSeconds": 1.2096e+06,
				"name":                    "data-pipeline-data-processing-dlq",
				"region":                  "us-east-1",
				"sqsManagedSseEnabled":    true,
				"tags": map[string]interface{}{
					"Purpose":    "Dead Letter Queue",
					"__defaults": []interface{}{},
				},
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
					"Purpose":     "Dead Letter Queue",
					"__defaults":  []interface{}{},
				},
				"visibilityTimeoutSeconds": 30,
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"arn":                          "arn:aws:sqs:us-east-1:894850187425:data-pipeline-data-processing-dlq",
				"contentBasedDeduplication":    false,
				"deduplicationScope":           "",
				"delaySeconds":                 0,
				"fifoQueue":                    false,
				"fifoThroughputLimit":          "",
				"id":                           "https://sqs.us-east-1.amazonaws.com/894850187425/data-pipeline-data-processing-dlq",
				"kmsDataKeyReusePeriodSeconds": 300,
				"kmsMasterKeyId":               "",
				"maxMessageSize":               262144,
				"messageRetentionSeconds":      1.2096e+06,
				"name":                         "data-pipeline-data-processing-dlq",
				"namePrefix":                   "",
				"policy":                       "",
				"receiveWaitTimeSeconds":       0,
				"redriveAllowPolicy":           "",
				"redrivePolicy":                "",
				"region":                       "us-east-1",
				"sqsManagedSseEnabled":         true,
				"tags":                         map[string]interface{}{"Purpose": "Dead Letter Queue"},
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
					"Purpose":     "Dead Letter Queue",
				},
				"url":                      "https://sqs.us-east-1.amazonaws.com/894850187425/data-pipeline-data-processing-dlq",
				"visibilityTimeoutSeconds": 30,
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:sqs/queuePolicy:QueuePolicy::data_processing"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "policy": `{"Statement":[{"Action":"sqs:SendMessage","Condition":{"ArnEquals":{"aws:SourceArn":"arn:aws:sns:us-east-1:894850187425:data-pipeline-s3-notifications"}},"Effect":"Allow","Principal":{"Service":"sns.amazonaws.com"},"Resource":"arn:aws:sqs:us-east-1:894850187425:data-pipeline-data-processing","Sid":"AllowSNSMessages"}],"Version":"2012-10-17"}`,
				"queueUrl": "https://sqs.us-east-1.amazonaws.com/894850187425/data-pipeline-data-processing",
				"region":   "us-east-1",
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"id":       "https://sqs.us-east-1.amazonaws.com/894850187425/data-pipeline-data-processing",
				"policy":   `{"Statement":[{"Action":"sqs:SendMessage","Condition":{"ArnEquals":{"aws:SourceArn":"arn:aws:sns:us-east-1:894850187425:data-pipeline-s3-notifications"}},"Effect":"Allow","Principal":{"Service":"sns.amazonaws.com"},"Resource":"arn:aws:sqs:us-east-1:894850187425:data-pipeline-data-processing","Sid":"AllowSNSMessages"}],"Version":"2012-10-17"}`,
				"queueUrl": "https://sqs.us-east-1.amazonaws.com/894850187425/data-pipeline-data-processing",
				"region":   "us-east-1",
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:s3/bucket:Bucket::this"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "bucket": "data-pipeline-data-lake-894850187425",
				"grants": []interface{}{map[string]interface{}{
					"__defaults":  []interface{}{},
					"id":          "69c80caa37c265d93308334e41ddd9ee253ab9f1460124a3c64fa7e21f3ef5b3",
					"permissions": []interface{}{"FULL_CONTROL"},
					"type":        "CanonicalUser",
				}},
				"lifecycleRules": []interface{}{map[string]interface{}{
					"__defaults": []interface{}{},
					"enabled":    true,
					"id":         "transition-to-ia",
					"noncurrentVersionExpiration": map[string]interface{}{
						"__defaults": []interface{}{},
						"days":       365,
					},
					"transitions": []interface{}{
						map[string]interface{}{
							"__defaults":   []interface{}{},
							"days":         30,
							"storageClass": "STANDARD_IA",
						},
						map[string]interface{}{
							"__defaults":   []interface{}{},
							"days":         90,
							"storageClass": "GLACIER",
						},
					},
				}},
				"region":       "us-east-1",
				"requestPayer": "BucketOwner",
				"serverSideEncryptionConfiguration": map[string]interface{}{
					"__defaults": []interface{}{},
					"rule": map[string]interface{}{
						"__defaults": []interface{}{},
						"applyServerSideEncryptionByDefault": map[string]interface{}{
							"__defaults":   []interface{}{},
							"sseAlgorithm": "aws:kms",
						},
						"bucketKeyEnabled": true,
					},
				},
				"tags": map[string]interface{}{
					"Purpose":    "Data Lake Storage",
					"__defaults": []interface{}{},
				},
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
					"Purpose":     "Data Lake Storage",
					"__defaults":  []interface{}{},
				},
				"versioning": map[string]interface{}{
					"__defaults": []interface{}{},
					"enabled":    true,
				},
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"accelerationStatus": "", "acl": nil, "arn": "arn:aws:s3:::data-pipeline-data-lake-894850187425",
				"bucket":                   "data-pipeline-data-lake-894850187425",
				"bucketDomainName":         "data-pipeline-data-lake-894850187425.s3.amazonaws.com",
				"bucketPrefix":             "",
				"bucketRegion":             "us-east-1",
				"bucketRegionalDomainName": "data-pipeline-data-lake-894850187425.s3.us-east-1.amazonaws.com",
				"corsRules":                []interface{}{},
				"forceDestroy":             false,
				"grants": []interface{}{map[string]interface{}{
					"id":          "69c80caa37c265d93308334e41ddd9ee253ab9f1460124a3c64fa7e21f3ef5b3",
					"permissions": []interface{}{"FULL_CONTROL"},
					"type":        "CanonicalUser",
					"uri":         "",
				}},
				"hostedZoneId": "Z3AQBSTGFYJSTF",
				"id":           "data-pipeline-data-lake-894850187425",
				"lifecycleRules": []interface{}{map[string]interface{}{
					"abortIncompleteMultipartUploadDays": 0,
					"enabled":                            true,
					"expiration":                         nil,
					"id":                                 "transition-to-ia",
					"noncurrentVersionExpiration":        map[string]interface{}{"days": 365},
					"noncurrentVersionTransitions":       []interface{}{},
					"prefix":                             "",
					"tags":                               map[string]interface{}{},
					"transitions": []interface{}{
						map[string]interface{}{
							"date":         "",
							"days":         30,
							"storageClass": "STANDARD_IA",
						},
						map[string]interface{}{
							"date":         "",
							"days":         90,
							"storageClass": "GLACIER",
						},
					},
				}},
				"logging":                  nil,
				"objectLockConfiguration":  nil,
				"objectLockEnabled":        false,
				"policy":                   "",
				"region":                   "us-east-1",
				"replicationConfiguration": nil,
				"requestPayer":             "BucketOwner",
				"serverSideEncryptionConfiguration": map[string]interface{}{"rule": map[string]interface{}{
					"applyServerSideEncryptionByDefault": map[string]interface{}{
						"kmsMasterKeyId": "",
						"sseAlgorithm":   "aws:kms",
					},
					"bucketKeyEnabled": true,
				}},
				"tags": map[string]interface{}{"Purpose": "Data Lake Storage"},
				"tagsAll": map[string]interface{}{
					"Environment": "dev",
					"ManagedBy":   "Terraform",
					"Project":     "data-pipeline",
					"Purpose":     "Data Lake Storage",
				},
				"versioning": map[string]interface{}{
					"enabled":   true,
					"mfaDelete": false,
				},
				"website":         nil,
				"websiteDomain":   nil,
				"websiteEndpoint": nil,
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:s3/bucketLifecycleConfiguration:BucketLifecycleConfiguration::this"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "bucket": "data-pipeline-data-lake-894850187425",
				"region": "us-east-1",
				"rules": []interface{}{map[string]interface{}{
					"__defaults": []interface{}{},
					"id":         "transition-to-ia",
					"noncurrentVersionExpiration": map[string]interface{}{
						"__defaults":     []interface{}{},
						"noncurrentDays": 365,
					},
					"status": "Enabled",
					"transitions": []interface{}{
						map[string]interface{}{
							"__defaults":   []interface{}{},
							"days":         30,
							"storageClass": "STANDARD_IA",
						},
						map[string]interface{}{
							"__defaults":   []interface{}{},
							"days":         90,
							"storageClass": "GLACIER",
						},
					},
				}},
				"transitionDefaultMinimumObjectSize": "all_storage_classes_128K",
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"bucket": "data-pipeline-data-lake-894850187425", "expectedBucketOwner": "",
				"id":     "data-pipeline-data-lake-894850187425",
				"region": "us-east-1",
				"rules": []interface{}{map[string]interface{}{
					"abortIncompleteMultipartUpload": nil,
					"expiration":                     nil,
					"filter": map[string]interface{}{
						"and":                   nil,
						"objectSizeGreaterThan": nil,
						"objectSizeLessThan":    nil,
						"prefix":                "",
						"tag":                   nil,
					},
					"id": "transition-to-ia",
					"noncurrentVersionExpiration": map[string]interface{}{
						"newerNoncurrentVersions": nil,
						"noncurrentDays":          365,
					},
					"noncurrentVersionTransitions": []interface{}{},
					"prefix":                       "",
					"status":                       "Enabled",
					"transitions": []interface{}{
						map[string]interface{}{
							"date":         nil,
							"days":         30,
							"storageClass": "STANDARD_IA",
						},
						map[string]interface{}{
							"date":         nil,
							"days":         90,
							"storageClass": "GLACIER",
						},
					},
				}},
				"transitionDefaultMinimumObjectSize": "all_storage_classes_128K",
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:s3/bucketOwnershipControls:BucketOwnershipControls::this"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "bucket": "data-pipeline-data-lake-894850187425",
				"region": "us-east-1",
				"rule": map[string]interface{}{
					"__defaults":      []interface{}{},
					"objectOwnership": "BucketOwnerEnforced",
				},
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"bucket": "data-pipeline-data-lake-894850187425", "id": "data-pipeline-data-lake-894850187425",
				"region": "us-east-1",
				"rule":   map[string]interface{}{"objectOwnership": "BucketOwnerEnforced"},
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:s3/bucketPublicAccessBlock:BucketPublicAccessBlock::this"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "blockPublicAcls": true, "blockPublicPolicy": true,
				"bucket":                "data-pipeline-data-lake-894850187425",
				"ignorePublicAcls":      true,
				"region":                "us-east-1",
				"restrictPublicBuckets": true,
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"blockPublicAcls": true, "blockPublicPolicy": true,
				"bucket":                "data-pipeline-data-lake-894850187425",
				"id":                    "data-pipeline-data-lake-894850187425",
				"ignorePublicAcls":      true,
				"region":                "us-east-1",
				"restrictPublicBuckets": true,
				"skipDestroy":           nil,
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:s3/bucketServerSideEncryptionConfiguration:BucketServerSideEncryptionConfiguration::this"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "bucket": "data-pipeline-data-lake-894850187425",
				"region": "us-east-1",
				"rules": []interface{}{map[string]interface{}{
					"__defaults": []interface{}{},
					"applyServerSideEncryptionByDefault": map[string]interface{}{
						"__defaults":   []interface{}{},
						"sseAlgorithm": "aws:kms",
					},
					"bucketKeyEnabled": true,
				}},
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"bucket": "data-pipeline-data-lake-894850187425", "expectedBucketOwner": "",
				"id":     "data-pipeline-data-lake-894850187425",
				"region": "us-east-1",
				"rules": []interface{}{map[string]interface{}{
					"applyServerSideEncryptionByDefault": map[string]interface{}{
						"kmsMasterKeyId": "",
						"sseAlgorithm":   "aws:kms",
					},
					"bucketKeyEnabled": true,
				}},
			}),
		},
		{
			resourceURN: urn.URN("urn:pulumi:dev::example::aws:s3/bucketVersioning:BucketVersioning::this"),
			expectInputs: autogold.Expect(map[string]interface{}{
				"__defaults": []interface{}{}, "bucket": "data-pipeline-data-lake-894850187425",
				"region": "us-east-1",
				"versioningConfiguration": map[string]interface{}{
					"__defaults": []interface{}{},
					"status":     "Enabled",
				},
			}),
			expectOutputs: autogold.Expect(map[string]interface{}{
				"bucket": "data-pipeline-data-lake-894850187425", "expectedBucketOwner": "",
				"id":     "data-pipeline-data-lake-894850187425",
				"mfa":    nil,
				"region": "us-east-1",
				"versioningConfiguration": map[string]interface{}{
					"mfaDelete": "",
					"status":    "Enabled",
				},
			}),
		},
	}

	for _, tc := range testCases {
		r := findByURN(string(tc.resourceURN), data.Deployment.Resources)
		require.NotNilf(t, r, "No resource by URN %v", tc.resourceURN)
		tc.expectInputs.Equal(t, r.Inputs)
		tc.expectOutputs.Equal(t, r.Outputs)
	}
}

func findByURN(urn string, resources []apitype.ResourceV3) *apitype.ResourceV3 {
	for _, r := range resources {
		if string(r.URN) == urn {
			return &r
		}
	}
	return nil
}

func randomSuffix() string {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = letters[rng.Intn(len(letters))]
	}
	return string(b)
}

func setupMinimalPulumiTestProject(t *testing.T) auto.Workspace {
	t.Helper()

	// Create a temporary directory for the test project
	tempDir := t.TempDir()

	// Create a minimal Pulumi.yaml project file
	pulumiYaml := `name: test-project
runtime: yaml
`
	err := os.WriteFile(filepath.Join(tempDir, "Pulumi.yaml"), []byte(pulumiYaml), 0644)
	assert.NoError(t, err)

	// Use automation API to create a stack and perform initial setup
	ctx := context.Background()

	// Create a new workspace with filestate backend
	stateDir := filepath.Join(tempDir, ".pulumi")
	err = os.MkdirAll(stateDir, 0755)
	require.NoError(t, err)

	// Set up environment to use local filestate backend
	workspace, err := auto.NewLocalWorkspace(ctx,
		auto.WorkDir(tempDir),
		auto.EnvVars(map[string]string{
			"PULUMI_BACKEND_URL":       "file://" + stateDir,
			"PULUMI_CONFIG_PASSPHRASE": "test",
		}),
	)
	require.NoError(t, err)

	// Create a new stack with a random suffix to avoid conflicts
	stackName := "test-" + randomSuffix()
	stack, err := auto.NewStack(ctx, stackName, workspace)
	require.NoError(t, err)

	// Perform an initial up to initialize the deployment
	// This creates an empty stack with just the root resource
	_, err = stack.Up(ctx)
	require.NoError(t, err)

	return workspace
}
