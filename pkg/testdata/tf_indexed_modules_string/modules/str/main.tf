variable "prefix" {
  type = string
}

resource "random_string" "this" {
  length  = 8
  special = false
  upper   = false
}

output "value" {
  value = "${var.prefix}-${random_string.this.result}"
}
