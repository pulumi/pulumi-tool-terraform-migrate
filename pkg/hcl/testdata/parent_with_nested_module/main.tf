variable "database_identifier" { default = "mydb" }
module "parent" {
  source  = "./modules/parent"
  db_name = var.database_identifier
}
