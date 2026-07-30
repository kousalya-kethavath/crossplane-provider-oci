//go:build nofork

/*
 * Copyright (c) 2026 Oracle and/or its affiliates
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"testing"

	ujconfig "github.com/crossplane/upjet/v2/pkg/config"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

const (
	retainedMemoryModeEnv    = "NOFORK_RETAINED_MEMORY_MODE"
	retainedMemoryProfileEnv = "NOFORK_RETAINED_MEMORY_PROFILE"
)

var retainedMemoryProvider *ujconfig.Provider

func TestRuntimeTerraformResourceManifests(t *testing.T) {
	allResources := map[string]string{}
	for service, resources := range runtimeTerraformResources {
		if service == "monolith" {
			continue
		}
		if len(resources) == 0 {
			t.Errorf("service %q has an empty runtime resource manifest", service)
		}
		for _, resourceName := range resources {
			if previousService, ok := allResources[resourceName]; ok {
				t.Errorf("Terraform resource %q is assigned to both %q and %q", resourceName, previousService, service)
			}
			allResources[resourceName] = service
		}
	}

	monolithResources := runtimeTerraformResources["monolith"]
	if len(monolithResources) != len(allResources) {
		t.Fatalf("monolith manifest has %d resources, want %d", len(monolithResources), len(allResources))
	}
	for resourceName := range allResources {
		if !slices.Contains(monolithResources, resourceName) {
			t.Errorf("monolith manifest does not contain %q", resourceName)
		}
	}
}

func TestRuntimeProviderContainsOnlyServiceResources(t *testing.T) {
	const service = "events"
	want := runtimeTerraformResources[service]
	if len(want) == 0 {
		t.Fatalf("service %q has no generated Terraform resources", service)
	}

	provider := GetProviderForRuntime(service)
	if len(provider.Resources) != len(want) {
		t.Fatalf("runtime provider has %d resources, want %d", len(provider.Resources), len(want))
	}
	if len(provider.TerraformProvider.ResourcesMap) != len(want) {
		t.Fatalf("runtime Terraform provider has %d resources, want %d", len(provider.TerraformProvider.ResourcesMap), len(want))
	}
	if len(provider.TerraformProvider.DataSourcesMap) != 0 {
		t.Fatalf("runtime Terraform provider has %d data sources, want 0", len(provider.TerraformProvider.DataSourcesMap))
	}
	predicate := SDKv2ResourcePredicateForRuntime(service)
	for _, resourceName := range want {
		if provider.Resources[resourceName] == nil {
			t.Errorf("runtime provider does not contain %q", resourceName)
		}
		if !predicate(resourceName) {
			t.Errorf("runtime SDKv2 predicate rejected %q", resourceName)
		}
	}
	if predicate("oci_core_vcn") {
		t.Error("events runtime SDKv2 predicate accepted unrelated resource oci_core_vcn")
	}
}

func BenchmarkProviderConstructionFull(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = GetProvider()
	}
}

func BenchmarkProviderConstructionEvents(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = GetProviderForRuntime("events")
	}
}

func BenchmarkProviderConstructionNetworking(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = GetProviderForRuntime("networking")
	}
}

// TestRetainedProviderMemory is an opt-in diagnostic that measures memory
// retained after constructing a full or service-filtered provider. Run each
// mode in a separate test process to keep the measurements independent. Set
// NOFORK_RETAINED_MEMORY_PROFILE to write an optional Go heap profile.
func TestRetainedProviderMemory(t *testing.T) {
	mode := os.Getenv(retainedMemoryModeEnv)
	if mode == "" {
		t.Skipf("set %s to baseline, full, or a generated service name", retainedMemoryModeEnv)
	}

	debug.FreeOSMemory()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	resourceCount := 0
	switch mode {
	case "baseline":
		retainedMemoryProvider = nil
	case "full":
		retainedMemoryProvider = GetProvider()
	default:
		if _, ok := runtimeTerraformResources[mode]; !ok || mode == "monolith" {
			t.Fatalf("unsupported %s value %q", retainedMemoryModeEnv, mode)
		}
		retainedMemoryProvider = GetProviderForRuntime(mode)
	}
	if retainedMemoryProvider != nil {
		resourceCount = len(retainedMemoryProvider.Resources)
	}

	debug.FreeOSMemory()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	rss, rssErr := currentRSSBytes()
	if rssErr != nil {
		t.Logf("current RSS unavailable: %v", rssErr)
	}

	t.Logf("RETAINED_MEMORY mode=%s resources=%d heap_alloc=%d heap_alloc_delta=%d heap_inuse=%d heap_inuse_delta=%d heap_sys=%d heap_released=%d sys=%d heap_objects=%d rss=%d",
		mode,
		resourceCount,
		after.HeapAlloc,
		int64(after.HeapAlloc)-int64(before.HeapAlloc),
		after.HeapInuse,
		int64(after.HeapInuse)-int64(before.HeapInuse),
		after.HeapSys,
		after.HeapReleased,
		after.Sys,
		after.HeapObjects,
		rss,
	)
	if profilePath := os.Getenv(retainedMemoryProfileEnv); profilePath != "" {
		if err := writeHeapProfile(profilePath); err != nil {
			t.Fatalf("write retained heap profile: %v", err)
		}
		t.Logf("RETAINED_MEMORY_PROFILE mode=%s path=%s", mode, profilePath)
	}
	runtime.KeepAlive(retainedMemoryProvider)
}

func writeHeapProfile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := pprof.WriteHeapProfile(file); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func currentRSSBytes() (uint64, error) {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		return 0, err
	}
	rssKiB, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, err
	}
	return rssKiB * 1024, nil
}

func TestProviderConfigIncludesPreviouslySkippedNoForkResources(t *testing.T) {
	for name, provider := range map[string]func() *ujconfig.Provider{
		"cluster":    GetProvider,
		"namespaced": GetProviderNamespaced,
	} {
		t.Run(name, func(t *testing.T) {
			pc := provider()

			savedSearch := pc.Resources["oci_management_dashboard_management_saved_search"]
			if savedSearch == nil {
				t.Fatal("ManagementSavedSearch resource was not generated")
			}
			if !savedSearch.ShouldUseTerraformPluginSDKClient() {
				t.Fatal("ManagementSavedSearch is not routed through SDKv2 no-fork")
			}

			opensearchCluster := pc.Resources["oci_opensearch_opensearch_cluster"]
			if opensearchCluster == nil {
				t.Fatal("OpensearchCluster resource was not generated")
			}
			if !opensearchCluster.ShouldUseTerraformPluginSDKClient() {
				t.Fatal("OpensearchCluster is not routed through SDKv2 no-fork")
			}

			requireGranularMapStrategies(t, pc, "oci_zpr_zpr_policy", "defined_tags", "freeform_tags")
			requireGranularMapStrategies(t, pc, "oci_core_vcn", "defined_tags", "freeform_tags", "security_attributes")
			requireGranularMapStrategies(t, pc, "oci_objectstorage_bucket", "defined_tags", "freeform_tags", "metadata")

			// Cover scalar maps inside nested list blocks and computed-only maps.
			// These schemas do not occur in the commonly tagged resources above.
			for _, resource := range []string{
				"oci_capacity_management_occ_capacity_request",
				"oci_data_safe_security_assessment_check",
				"oci_data_safe_security_assessment_finding",
				"oci_data_safe_sensitive_type_group_grouped_sensitive_type",
				"oci_demand_signal_occ_demand_signal",
				"oci_psql_db_system",
			} {
				requireGranularMapStrategies(t, pc, resource, "patch_operations.value")
			}
			requireGranularMapStrategies(t, pc, "oci_generic_artifacts_content_artifact_by_path", "defined_tags", "freeform_tags")
		})
	}
}

func TestGeneratedAPICompatibilityDoesNotMutateRuntimeSchema(t *testing.T) {
	const resourceName = "oci_adm_vulnerability_audit"
	const integerField = "vulnerable_artifacts_count"

	runtimeResource := GetProvider().Resources[resourceName]
	if runtimeResource == nil {
		t.Fatalf("runtime resource %q is missing", resourceName)
	}
	if got := runtimeResource.TerraformResource.Schema[integerField].Type; got != schema.TypeInt {
		t.Fatalf("runtime %s.%s type = %s, want TypeInt", resourceName, integerField, got)
	}

	generatedResource := GetProviderForGeneration().Resources[resourceName]
	if generatedResource == nil {
		t.Fatalf("generation resource %q is missing", resourceName)
	}
	if got := generatedResource.TerraformResource.Schema[integerField].Type; got != schema.TypeFloat {
		t.Fatalf("generation %s.%s type = %s, want TypeFloat", resourceName, integerField, got)
	}

	const mapResourceName = "oci_objectstorage_object"
	const mapField = "metadata"
	runtimeMap := GetProvider().Resources[mapResourceName].TerraformResource.Schema[mapField]
	if _, ok := runtimeMap.Elem.(schema.ValueType); !ok {
		t.Fatalf("runtime %s.%s element = %T, want schema.ValueType", mapResourceName, mapField, runtimeMap.Elem)
	}
	generationMap := GetProviderForGeneration().Resources[mapResourceName].TerraformResource.Schema[mapField]
	if _, ok := generationMap.Elem.(*schema.Schema); !ok {
		t.Fatalf("generation %s.%s element = %T, want *schema.Schema", mapResourceName, mapField, generationMap.Elem)
	}
}

func TestEmbeddedSDKProviderInstancesDoNotShareTopLevelMaps(t *testing.T) {
	first := terraformSDKProvider(nil)
	second := terraformSDKProvider(nil)
	if first == second {
		t.Fatal("terraformSDKProvider returned the same provider instance")
	}

	const resourceName = "oci_core_vcn"
	if first.ResourcesMap[resourceName] == nil || second.ResourcesMap[resourceName] == nil {
		t.Fatalf("expected %q in both provider resource maps", resourceName)
	}
	delete(first.ResourcesMap, resourceName)
	if second.ResourcesMap[resourceName] == nil {
		t.Fatalf("deleting %q from one provider changed another provider's resource map", resourceName)
	}

	const datasourceName = "oci_core_vcns"
	if first.DataSourcesMap[datasourceName] == nil || second.DataSourcesMap[datasourceName] == nil {
		t.Fatalf("expected %q in both provider data source maps", datasourceName)
	}
	delete(first.DataSourcesMap, datasourceName)
	if second.DataSourcesMap[datasourceName] == nil {
		t.Fatalf("deleting %q from one provider changed another provider's data source map", datasourceName)
	}
}

func requireGranularMapStrategies(t *testing.T, pc *ujconfig.Provider, resource string, fields ...string) {
	t.Helper()
	r := pc.Resources[resource]
	if r == nil {
		t.Fatalf("%s resource was not generated", resource)
	}
	for _, field := range fields {
		strategy, ok := r.ServerSideApplyMergeStrategies[field]
		if !ok {
			t.Fatalf("%s %s server-side apply merge strategy is missing", resource, field)
		}
		if strategy.MapMergeStrategy != ujconfig.MapTypeGranular {
			t.Fatalf("%s %s map merge strategy = %q, want %q", resource, field, strategy.MapMergeStrategy, ujconfig.MapTypeGranular)
		}
	}
}
