variable "enabled" { type = bool; default = true }
output "status" { value = var.enabled ? "active" : "inactive" }
