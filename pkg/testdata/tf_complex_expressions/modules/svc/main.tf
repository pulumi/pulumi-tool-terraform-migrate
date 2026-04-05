variable "prefix" {
  type = string
}

variable "is_primary" {
  type    = bool
  default = false
}

variable "label" {
  type    = string
  default = "default"
}

resource "random_pet" "this" {
  prefix = var.prefix
}

output "name" {
  value = random_pet.this.id
}
