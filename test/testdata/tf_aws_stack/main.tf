# =============================================================================
# AWS Terraform Configuration with S3, Lambda, and Module Usage
# =============================================================================

terraform {
  required_version = ">= 1.0.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "6.19.0"
    }
  }
}

# -----------------------------------------------------------------------------
# Provider Configuration
# -----------------------------------------------------------------------------
provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Project     = var.project_name
      Environment = var.environment
      ManagedBy   = "Terraform"
    }
  }
}

# -----------------------------------------------------------------------------
# Variables
# -----------------------------------------------------------------------------
variable "aws_region" {
  description = "AWS region for resources"
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Name of the project"
  type        = string
  default     = "data-pipeline4"
}

variable "environment" {
  description = "Environment name"
  type        = string
  default     = "dev"
}

# -----------------------------------------------------------------------------
# Data Sources
# -----------------------------------------------------------------------------
data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

# -----------------------------------------------------------------------------
# S3 Bucket Module - Using terraform-aws-modules/s3-bucket
# This is the required Terraform module
# -----------------------------------------------------------------------------
module "data_lake_bucket" {
  source  = "terraform-aws-modules/s3-bucket/aws"
  version = "~> 4.0"

  bucket = "${var.project_name}-data-lake-${data.aws_caller_identity.current.account_id}"

  # Bucket ownership and ACL
  control_object_ownership = true
  object_ownership         = "BucketOwnerEnforced"

  # Versioning
  versioning = {
    enabled = true
  }

  # Server-side encryption
  server_side_encryption_configuration = {
    rule = {
      apply_server_side_encryption_by_default = {
        sse_algorithm = "aws:kms"
      }
      bucket_key_enabled = true
    }
  }

  # Lifecycle rules
  lifecycle_rule = [
    {
      id      = "transition-to-ia"
      enabled = true

      transition = [
        {
          days          = 30
          storage_class = "STANDARD_IA"
        },
        {
          days          = 90
          storage_class = "GLACIER"
        }
      ]

      noncurrent_version_expiration = {
        days = 365
      }
    }
  ]

  # Block public access
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true

  tags = {
    Purpose = "Data Lake Storage"
  }
}

# -----------------------------------------------------------------------------
# Additional S3 Bucket - For Lambda Deployment Packages
# -----------------------------------------------------------------------------
resource "aws_s3_bucket" "lambda_artifacts" {
  bucket = "${var.project_name}-lambda-artifacts-${data.aws_caller_identity.current.account_id}"

  tags = {
    Purpose = "Lambda Deployment Artifacts"
  }
}

resource "aws_s3_bucket_versioning" "lambda_artifacts" {
  bucket = aws_s3_bucket.lambda_artifacts.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "lambda_artifacts" {
  bucket = aws_s3_bucket.lambda_artifacts.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "lambda_artifacts" {
  bucket = aws_s3_bucket.lambda_artifacts.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# -----------------------------------------------------------------------------
# SNS Topic for S3 Event Notifications
# -----------------------------------------------------------------------------
resource "aws_sns_topic" "s3_notifications" {
  name = "${var.project_name}-s3-notifications"

  tags = {
    Purpose = "S3 Event Notifications"
  }
}

resource "aws_sns_topic_policy" "s3_notifications" {
  arn = aws_sns_topic.s3_notifications.arn

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "AllowS3Publish"
        Effect = "Allow"
        Principal = {
          Service = "s3.amazonaws.com"
        }
        Action   = "sns:Publish"
        Resource = aws_sns_topic.s3_notifications.arn
        Condition = {
          ArnLike = {
            "aws:SourceArn" = module.data_lake_bucket.s3_bucket_arn
          }
        }
      }
    ]
  })
}

# -----------------------------------------------------------------------------
# SQS Queue for Processing
# -----------------------------------------------------------------------------
resource "aws_sqs_queue" "data_processing" {
  name                       = "${var.project_name}-data-processing"
  visibility_timeout_seconds = 300
  message_retention_seconds  = 86400
  receive_wait_time_seconds  = 10

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.data_processing_dlq.arn
    maxReceiveCount     = 3
  })

  tags = {
    Purpose = "Data Processing Queue"
  }
}

resource "aws_sqs_queue" "data_processing_dlq" {
  name                      = "${var.project_name}-data-processing-dlq"
  message_retention_seconds = 1209600 # 14 days

  tags = {
    Purpose = "Dead Letter Queue"
  }
}

resource "aws_sqs_queue_policy" "data_processing" {
  queue_url = aws_sqs_queue.data_processing.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "AllowSNSMessages"
        Effect = "Allow"
        Principal = {
          Service = "sns.amazonaws.com"
        }
        Action   = "sqs:SendMessage"
        Resource = aws_sqs_queue.data_processing.arn
        Condition = {
          ArnEquals = {
            "aws:SourceArn" = aws_sns_topic.s3_notifications.arn
          }
        }
      }
    ]
  })
}

resource "aws_sns_topic_subscription" "sqs_subscription" {
  topic_arn = aws_sns_topic.s3_notifications.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.data_processing.arn
}

# -----------------------------------------------------------------------------
# IAM Role for Lambda Function
# -----------------------------------------------------------------------------
resource "aws_iam_role" "lambda_role" {
  name = "${var.project_name}-lambda-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "lambda.amazonaws.com"
        }
      }
    ]
  })

  tags = {
    Purpose = "Lambda Execution Role"
  }
}

resource "aws_iam_role_policy" "lambda_s3_access" {
  name = "${var.project_name}-lambda-s3-access"
  role = aws_iam_role.lambda_role.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "s3:GetObject",
          "s3:PutObject",
          "s3:DeleteObject",
          "s3:ListBucket"
        ]
        Resource = [
          module.data_lake_bucket.s3_bucket_arn,
          "${module.data_lake_bucket.s3_bucket_arn}/*"
        ]
      },
      {
        Effect = "Allow"
        Action = [
          "sqs:ReceiveMessage",
          "sqs:DeleteMessage",
          "sqs:GetQueueAttributes"
        ]
        Resource = aws_sqs_queue.data_processing.arn
      },
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ]
        Resource = "arn:aws:logs:${data.aws_region.current.name}:${data.aws_caller_identity.current.account_id}:*"
      }
    ]
  })
}

# -----------------------------------------------------------------------------
# Lambda Function for Data Processing
# -----------------------------------------------------------------------------
data "archive_file" "lambda_placeholder" {
  type        = "zip"
  output_path = "${path.module}/lambda_function.zip"

  source {
    content  = <<-EOF
      import json
      import boto3

      def handler(event, context):
          print(f"Received event: {json.dumps(event)}")
          
          s3_client = boto3.client('s3')
          
          for record in event.get('Records', []):
              body = json.loads(record['body'])
              message = json.loads(body.get('Message', '{}'))
              
              for s3_record in message.get('Records', []):
                  bucket = s3_record['s3']['bucket']['name']
                  key = s3_record['s3']['object']['key']
                  print(f"Processing: s3://{bucket}/{key}")
          
          return {
              'statusCode': 200,
              'body': json.dumps('Processing complete')
          }
    EOF
    filename = "lambda_function.py"
  }
}

resource "aws_lambda_function" "data_processor" {
  function_name = "${var.project_name}-data-processor"
  role          = aws_iam_role.lambda_role.arn
  handler       = "lambda_function.handler"
  runtime       = "python3.11"
  timeout       = 60
  memory_size   = 256

  filename         = data.archive_file.lambda_placeholder.output_path
  source_code_hash = data.archive_file.lambda_placeholder.output_base64sha256

  environment {
    variables = {
      DATA_LAKE_BUCKET = module.data_lake_bucket.s3_bucket_id
      ENVIRONMENT      = var.environment
    }
  }

  tags = {
    Purpose = "Data Processing"
  }
}

resource "aws_lambda_event_source_mapping" "sqs_trigger" {
  event_source_arn = aws_sqs_queue.data_processing.arn
  function_name    = aws_lambda_function.data_processor.arn
  batch_size       = 10
  enabled          = true
}

resource "aws_cloudwatch_log_group" "lambda_logs" {
  name              = "/aws/lambda/${aws_lambda_function.data_processor.function_name}"
  retention_in_days = 14

  tags = {
    Purpose = "Lambda Logs"
  }
}

# -----------------------------------------------------------------------------
# DynamoDB Table for Metadata Tracking
# -----------------------------------------------------------------------------
resource "aws_dynamodb_table" "file_metadata" {
  name         = "${var.project_name}-file-metadata"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "file_id"
  range_key    = "timestamp"

  attribute {
    name = "file_id"
    type = "S"
  }

  attribute {
    name = "timestamp"
    type = "N"
  }

  attribute {
    name = "status"
    type = "S"
  }

  global_secondary_index {
    name            = "status-index"
    hash_key        = "status"
    range_key       = "timestamp"
    projection_type = "ALL"
  }

  point_in_time_recovery {
    enabled = true
  }

  tags = {
    Purpose = "File Metadata Tracking"
  }
}

# -----------------------------------------------------------------------------
# Outputs
# -----------------------------------------------------------------------------
output "data_lake_bucket_name" {
  description = "Name of the data lake S3 bucket"
  value       = module.data_lake_bucket.s3_bucket_id
}

output "data_lake_bucket_arn" {
  description = "ARN of the data lake S3 bucket"
  value       = module.data_lake_bucket.s3_bucket_arn
}

output "lambda_artifacts_bucket_name" {
  description = "Name of the Lambda artifacts S3 bucket"
  value       = aws_s3_bucket.lambda_artifacts.id
}

output "lambda_function_arn" {
  description = "ARN of the data processor Lambda function"
  value       = aws_lambda_function.data_processor.arn
}

output "sqs_queue_url" {
  description = "URL of the data processing SQS queue"
  value       = aws_sqs_queue.data_processing.url
}

output "dynamodb_table_name" {
  description = "Name of the DynamoDB metadata table"
  value       = aws_dynamodb_table.file_metadata.name
}

