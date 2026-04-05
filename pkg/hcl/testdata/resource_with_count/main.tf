resource "aws_route_table_association" "redshift" {
  count          = 0
  subnet_id      = "subnet-xxx"
  route_table_id = "rtb-xxx"
}

output "redshift_route_table_association_ids" {
  value = aws_route_table_association.redshift[*].id
}
