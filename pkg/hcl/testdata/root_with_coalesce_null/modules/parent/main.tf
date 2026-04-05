variable "optional_name" {
  type    = string
  default = null
}
variable "fallback_name" {
  type    = string
  default = "fallback"
}
module "child" {
  source = "../child"
  name   = coalesce(var.optional_name, var.fallback_name)
}
