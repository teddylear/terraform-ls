variable "test" {
}

output "foo" {
  value = "${var.test}-${var.test}"
}
