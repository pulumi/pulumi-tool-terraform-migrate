terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region                   = "us-east-1"
  shared_config_files      = []
  shared_credentials_files = []
}

module "bucket" {
  count  = 2
  source = "./modules/bucket"
  name   = "pulumi-migrate-test-${count.index}"
}
