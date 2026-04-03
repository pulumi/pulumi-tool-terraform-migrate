variable "env" {
  type    = string
  default = "dev"
}

variable "project" {
  type    = string
  default = "myapp"
}

locals {
  name       = "${var.project}-${var.env}"
  upper_name = upper(local.name)
  tags = {
    environment = var.env
    project     = var.project
    name        = local.name
  }
}

module "pet" {
  source = "../pet_module"
  prefix = local.name
}
