variable "name" { type = string }
module "svc" {
  count  = 2
  source = "../svc"
  prefix = "${var.name}-svc-${count.index}"
}
output "name" { value = var.name }
