variable "prefix" {
  type        = string
  description = "Prefix for the pet name"
}

variable "separator" {
  type    = string
  default = "-"
}

variable "length" {
  type    = number
  default = 2
}
