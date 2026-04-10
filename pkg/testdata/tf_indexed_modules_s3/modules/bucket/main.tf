variable "name" {
  type = string
}

resource "aws_s3_bucket" "this" {
  bucket = var.name

  tags = {
    ManagedBy = "terraform"
    Purpose   = "migration-test"
  }
}

output "arn" {
  value = aws_s3_bucket.this.arn
}

output "id" {
  value = aws_s3_bucket.this.id
}
