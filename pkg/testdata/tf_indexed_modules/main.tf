module "pet" {
  count  = 2
  source = "./modules/pet"
  prefix = "test-${count.index}"
}
