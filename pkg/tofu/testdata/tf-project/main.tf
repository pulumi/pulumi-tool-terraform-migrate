terraform {
  required_providers {
    random = {
      source  = "hashicorp/random"
      version = "~> 3.0"
    }
  }
}

resource "random_string" "example" {
  length  = 16
  special = true
  upper   = true
  lower   = true
  numeric = true
}

output "random_string_value" {
  value       = random_string.example.result
  description = "The generated random string"
}
