variable "prefix" {
  type = string
}

variable "suffix" {
  type    = string
  default = "prod"
}

resource "random_pet" "this" {
  prefix = "${var.prefix}-${var.suffix}"
}

output "name" {
  value = random_pet.this.id
}
