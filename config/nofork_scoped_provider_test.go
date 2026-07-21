//go:build nofork && nofork_scoped

package config

import "testing"

func TestScopedProviderMatchesEffectiveServiceRouting(t *testing.T) {
	if serviceScope == "" {
		t.Fatal("serviceScope is empty; set it with -ldflags -X github.com/oracle/provider-oci/config.serviceScope=<service>")
	}

	cluster := GetProvider()
	namespaced := GetProviderNamespaced()
	if len(cluster.Resources) == 0 {
		t.Fatalf("service %s has no configured resources", serviceScope)
	}
	if len(cluster.Resources) != len(namespaced.Resources) {
		t.Fatalf("cluster resource count %d != namespaced resource count %d", len(cluster.Resources), len(namespaced.Resources))
	}
	if len(cluster.Resources) != len(cluster.TerraformProvider.ResourcesMap) {
		t.Fatalf("configured resource count %d != scoped SDKv2 registry count %d", len(cluster.Resources), len(cluster.TerraformProvider.ResourcesMap))
	}
	if cluster.TerraformPluginFrameworkProvider != nil {
		t.Fatal("scoped provider unexpectedly configured a Terraform Framework provider")
	}

	for name, resource := range cluster.Resources {
		if resource.ShortGroup != serviceScope {
			t.Fatalf("resource %s uses group %s, want %s", name, resource.ShortGroup, serviceScope)
		}
		if _, ok := namespaced.Resources[name]; !ok {
			t.Fatalf("resource %s is missing from namespaced provider", name)
		}
		if cluster.TerraformProvider.ResourcesMap[name] == nil {
			t.Fatalf("resource %s is missing from scoped SDKv2 registry", name)
		}
	}
}
