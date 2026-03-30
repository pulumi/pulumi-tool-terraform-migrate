terraform {
  backend "remote" {
    hostname     = "api.pulumi.com"
    organization = "pulumi"
    workspaces {
      name = "tf-migrate-test_single-ws"
    }
  }
  required_providers {
    random = {
      source  = "hashicorp/random"
      version = "~> 3.0"
    }
  }
}

resource "random_string" "example" {
  length = 16
}
