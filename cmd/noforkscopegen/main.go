package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	ujconfig "github.com/crossplane/upjet/v2/pkg/config"

	providerconfig "github.com/oracle/provider-oci/config"
	"github.com/oracle/provider-oci/internal/noforkscope"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "noforkscopegen: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("noforkscopegen", flag.ContinueOnError)
	providerDir := fs.String("provider-dir", filepath.Join(root, ".work", "nofork", "terraform-provider-oci"), "prepared terraform-provider-oci directory")
	reportPath := fs.String("report", "", "service scope report path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *reportPath == "" {
		*reportPath = filepath.Join(*providerDir, "nofork-service-scopes.json")
	}

	serviceResources, err := effectiveServiceResources(root)
	if err != nil {
		return err
	}
	report, err := noforkscope.Generate(*providerDir, *reportPath, serviceResources)
	if err != nil {
		return err
	}

	resourceCount := 0
	for _, scope := range report {
		resourceCount += len(scope.Resources)
	}
	fmt.Printf("==> Generated %d service-scoped OCI providers for %d configured resources\n", len(report), resourceCount)
	fmt.Printf("==> Service scope report: %s\n", *reportPath)
	return nil
}

func effectiveServiceResources(root string) (map[string][]string, error) {
	services, err := serviceCommandDirectories(root)
	if err != nil {
		return nil, err
	}
	cluster := configuredResourceGroups(providerconfig.GetProvider())
	namespaced := configuredResourceGroups(providerconfig.GetProviderNamespaced())
	if err := compareConfiguredScopes(cluster, namespaced); err != nil {
		return nil, err
	}

	for resource, service := range cluster {
		detected, _ := providerconfig.ResourceGroupKind(resource)
		if detected != service {
			return nil, fmt.Errorf("configured resource %s uses service %s but groups.go resolves %s", resource, service, detected)
		}
		if _, ok := services[service]; !ok {
			return nil, fmt.Errorf("configured resource %s resolves to service %s without cmd/provider/%s", resource, service, service)
		}
		for _, scope := range []string{"cluster", "namespaced"} {
			apiDir := filepath.Join(root, "apis", scope, service)
			if info, err := os.Stat(apiDir); err != nil || !info.IsDir() {
				return nil, fmt.Errorf("configured service %s is missing generated API directory %s", service, apiDir)
			}
		}
		services[service] = append(services[service], resource)
	}
	for service := range services {
		sort.Strings(services[service])
	}
	return services, nil
}

func configuredResourceGroups(provider *ujconfig.Provider) map[string]string {
	resources := make(map[string]string, len(provider.Resources))
	for name, resource := range provider.Resources {
		resources[name] = resource.ShortGroup
	}
	return resources
}

func compareConfiguredScopes(cluster, namespaced map[string]string) error {
	if len(cluster) != len(namespaced) {
		return fmt.Errorf("cluster and namespaced configured resource counts differ: %d != %d", len(cluster), len(namespaced))
	}
	for resource, clusterService := range cluster {
		namespacedService, ok := namespaced[resource]
		if !ok {
			return fmt.Errorf("resource %s is configured only for cluster scope", resource)
		}
		if clusterService != namespacedService {
			return fmt.Errorf("resource %s maps to cluster service %s and namespaced service %s", resource, clusterService, namespacedService)
		}
	}
	return nil
}

func serviceCommandDirectories(root string) (map[string][]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "cmd", "provider"))
	if err != nil {
		return nil, fmt.Errorf("read provider command directories: %w", err)
	}
	services := map[string][]string{}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "config" || entry.Name() == "monolith" {
			continue
		}
		services[entry.Name()] = nil
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("no service provider command directories found")
	}
	return services, nil
}
