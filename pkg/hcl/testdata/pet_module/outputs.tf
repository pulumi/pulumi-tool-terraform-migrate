output "name" {
  value       = random_pet.this.id
  description = "The generated pet name"
}

output "separator" {
  value = random_pet.this.separator
}
