module "parent" {
  source = "./modules/parent"
}
module "consumer" {
  source = "./modules/consumer"
  name   = module.parent.child_val
}
