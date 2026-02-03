# Test Terraform state for random_string with schema_version 2 (current)
terraform {
  required_providers {
    random = {
      source  = "hashicorp/random"
      version = "~> 3.0"
    }
  }
}

resource "random_string" "current" {
  length  = 16
  special = true
  upper   = true
  lower   = true
  numeric = true
}

output "result" {
  value = random_string.current.result
}
