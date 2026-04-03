module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.1.0"
  cidr    = "10.0.0.0/16"
}

module "local_pet" {
  source = "./modules/pet"
  prefix = "test"
}
