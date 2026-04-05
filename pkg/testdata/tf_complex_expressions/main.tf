module "svc" {
  count      = 2
  source     = "./modules/svc"
  prefix     = join("-", ["svc", format("%02d", count.index)])
  is_primary = count.index == 0 ? true : false
  label      = upper("service-${count.index}")
}
