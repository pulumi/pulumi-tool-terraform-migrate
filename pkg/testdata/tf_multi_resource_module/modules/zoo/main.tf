variable "prefix" {
  type = string
}

resource "random_pet" "animal" {
  prefix = var.prefix
}

resource "random_string" "tag" {
  length  = 8
  special = false
}

resource "random_integer" "count" {
  min = 1
  max = 100
}

output "animal_name" {
  value = random_pet.animal.id
}

output "tag" {
  value = random_string.tag.result
}
