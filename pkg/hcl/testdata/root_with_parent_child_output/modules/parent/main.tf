module "child" {
  source = "../child"
}
output "child_val" { value = module.child.val }
