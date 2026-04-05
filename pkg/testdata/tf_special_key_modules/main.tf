module "region" {
  for_each = toset(["us-east-1", "eu-west-1/zone-a", "ap.southeast.2"])
  source   = "./modules/region"
  name     = each.key
}
