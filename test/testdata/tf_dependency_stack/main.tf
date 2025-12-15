resource "random_string" "random" {
  length           = 16
  special          = true
  override_special = "/@£$"
}

resource "random_string" "random2" {
  length           = 16
  special          = true
  override_special = "/@£$"
}

resource "random_string" "random3" {
  length           = 16
  special          = true
  override_special = "/@£$"
}

resource "random_shuffle" "random_shuffle" {
  input = [random_string.random.result, random_string.random2.result, random_string.random3.result]

  # chosen randomly
  seed = 3
}
