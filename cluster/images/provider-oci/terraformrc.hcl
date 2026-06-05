provider_installation {
  filesystem_mirror {
    path    = "/terraform/provider-mirror"
    include = ["registry.terraform.io/oracle/oci"]
  }
  direct {
    exclude = ["registry.terraform.io/oracle/oci"]
  }
}
