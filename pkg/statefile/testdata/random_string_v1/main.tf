# Test Terraform state for random_string with schema_version 1 (old)
# This uses an older provider version that has "number" instead of "numeric".
terraform {
  required_providers {
    random = {
      source  = "hashicorp/random"
      version = "2.3.0" # Old version with schema_version 1
    }
  }
}

resource "random_string" "legacy" {
  length  = 16
  special = true
  upper   = true
  lower   = true
  number  = true # Old attribute name (now "numeric")
}

output "result" {
  value = random_string.legacy.result
}
