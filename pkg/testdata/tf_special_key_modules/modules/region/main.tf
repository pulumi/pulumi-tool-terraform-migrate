variable "name" { type = string }
resource "random_pet" "this" { prefix = var.name }
output "pet_name" { value = random_pet.this.id }
