variable "env" {
  type = string
}

module "named" {
  source = "./modules/named"
  prefix = var.env
}
