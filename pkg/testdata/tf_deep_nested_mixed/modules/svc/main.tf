variable "prefix" { type = string }
module "instance" {
  source = "../instance"
  prefix = var.prefix
}
output "prefix" { value = var.prefix }
