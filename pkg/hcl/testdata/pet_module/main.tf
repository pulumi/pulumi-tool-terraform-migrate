resource "random_pet" "this" {
  prefix    = var.prefix
  separator = var.separator
  length    = var.length
}
