module "str" {
  count  = 2
  source = "./modules/str"
  prefix = "test-${count.index}"
}
