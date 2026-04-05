module "env" {
  for_each = toset(["dev", "prod"])
  source   = "./modules/env"
  name     = each.key
}
