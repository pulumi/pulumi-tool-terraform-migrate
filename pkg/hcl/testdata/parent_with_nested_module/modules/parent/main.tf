variable "db_name" { type = string }
module "child" {
  source = "../child"
  name   = var.db_name
}
output "child_name" { value = module.child.name }
