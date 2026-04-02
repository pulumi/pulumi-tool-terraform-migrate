variable "env" {
  type    = string
  default = "dev"
}

module "pet" {
  count  = 2
  source = "../pet_module"
  prefix = "test-${count.index}"
}

module "named_pet" {
  source    = "../pet_module"
  prefix    = var.env
  separator = "_"
  length    = 3
}
