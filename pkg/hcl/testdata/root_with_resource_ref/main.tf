resource "random_pet" "base" {
  prefix = "base"
}

module "consumer" {
  source    = "../pet_module"
  prefix    = random_pet.base.id
  separator = "_"
  length    = 3
}
