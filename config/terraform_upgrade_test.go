// Copyright (c) 2026 Oracle and/or its affiliates.
// Licensed under the Apache License, Version 2.0.

package config

import (
	"slices"
	"testing"
)

// Exercise newly introduced services as well as additions to existing services
// through the generated runtime manifests and the public embedding boundary.
func TestUpgradedResourcesSupportSelectiveNoForkConstruction(t *testing.T) {
	for _, name := range []string{
		"oci_core_drg_nat_policy",
		"oci_data_safe_crypto_assessment",
		"oci_dbmulticloud_oracle_db_aws_key_pool",
		"oci_generative_ai_routing_profile",
		"oci_oci_product_catalog_internal_admin_product",
		"oci_oci_product_catalog_internal_product",
	} {
		t.Run(name, func(t *testing.T) {
			if !slices.Contains(runtimeTerraformResources["monolith"], name) {
				t.Fatal("resource is missing from generated runtime manifest")
			}
			first := terraformSDKProvider([]string{name})
			second := terraformSDKProvider([]string{name})
			if len(first.ResourcesMap) != 1 || first.ResourcesMap[name] == nil {
				t.Fatal("selective provider did not construct the requested resource")
			}
			if len(first.DataSourcesMap) != 0 {
				t.Fatal("selective provider retained data sources")
			}
			if first.ResourcesMap[name] == second.ResourcesMap[name] {
				t.Fatal("resource schema is shared between provider instances")
			}
			if !IsSDKv2Resource(name) {
				t.Fatal("resource is not routed through SDKv2 no-fork")
			}
		})
	}
}
