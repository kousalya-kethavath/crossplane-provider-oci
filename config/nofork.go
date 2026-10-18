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
	"encoding/json"
	"regexp"
	"slices"
	"sort"
	"sync"

	ujconfig "github.com/crossplane/upjet/v2/pkg/config"
)

// terraformCLIIncludeList explicitly assigns resources to Upjet's CLI connector.
// These resources are excluded from the default SDKv2 routing. Changing a route
// requires regeneration and a runtime image with the corresponding executables.
var terraformCLIIncludeList = []string{}

// terraformPluginFrameworkIncludeList is intentionally empty until OCI
// Terraform Plugin Framework resources are validated for no-fork routing.
var terraformPluginFrameworkIncludeList = []string{}

type terraformProviderSchema struct {
	ProviderSchemas map[string]struct {
		ResourceSchemas map[string]json.RawMessage `json:"resource_schemas"`
	} `json:"provider_schemas"`
}

type terraformResourceRouting struct {
	names            []string
	sdkV2Resources   map[string]struct{}
	cliRegexes       []string
	frameworkRegexes []string
}

var loadTerraformResourceRouting = sync.OnceValue(func() terraformResourceRouting {
	return newTerraformResourceRouting(parseTerraformResourceNamesFromSchema(), terraformCLIIncludeList, terraformPluginFrameworkIncludeList)
})

func newTerraformResourceRouting(names, cliRegexes, frameworkRegexes []string) terraformResourceRouting {
	routing := terraformResourceRouting{
		names:            slices.Clone(names),
		sdkV2Resources:   make(map[string]struct{}, len(names)),
		cliRegexes:       slices.Clone(cliRegexes),
		frameworkRegexes: slices.Clone(frameworkRegexes),
	}
	for _, resource := range names {
		cli, framework := resourceMatches(resource, cliRegexes), resourceMatches(resource, frameworkRegexes)
		if cli && framework {
			panic("Terraform resource assigned to both CLI and Framework: " + resource)
		}
		if cli || framework {
			continue
		}
		routing.sdkV2Resources[resource] = struct{}{}
	}
	return routing
}

// includeLists restricts each execution route to the resources in a runtime.
// A nil manifest selects all schema resources during full-provider generation.
func (r terraformResourceRouting) includeLists(resources []string) (sdk, cli, framework []string) {
	if resources == nil {
		resources = r.names
	}
	for _, name := range resources {
		switch {
		case resourceMatches(name, r.cliRegexes):
			cli = append(cli, exactResourceRegex(name))
		case resourceMatches(name, r.frameworkRegexes):
			framework = append(framework, exactResourceRegex(name))
		default:
			if _, ok := r.sdkV2Resources[name]; ok {
				sdk = append(sdk, exactResourceRegex(name))
			}
		}
	}
	return sdk, cli, framework
}

// HasCLIResources reports whether this configured runtime needs CLI workspaces.
func HasCLIResources(provider *ujconfig.Provider) bool {
	for _, resource := range provider.Resources {
		if !resource.ShouldUseTerraformPluginSDKClient() && !resource.ShouldUseTerraformPluginFrameworkClient() {
			return true
		}
	}
	return false
}

func terraformPluginSDKResourcesForRuntime(service string) []string {
	resources, ok := runtimeTerraformResources[service]
	if !ok {
		panic("no Terraform runtime resource manifest for service " + service)
	}
	sdkResources := make([]string, 0, len(resources))
	for _, resource := range resources {
		if IsSDKv2Resource(resource) {
			sdkResources = append(sdkResources, resource)
		}
	}
	return sdkResources
}

// SDKv2ResourcePredicateForRuntime returns a predicate that accepts only the
// Terraform resources assigned to the specified service runtime.
func SDKv2ResourcePredicateForRuntime(service string) func(string) bool {
	resources := terraformPluginSDKResourcesForRuntime(service)
	resourceSet := make(map[string]struct{}, len(resources))
	for _, resource := range resources {
		resourceSet[resource] = struct{}{}
	}
	return func(terraformResourceType string) bool {
		_, ok := resourceSet[terraformResourceType]
		return ok
	}
}

// IsSDKv2Resource reports whether the Terraform resource type is routed through
// Upjet's in-process Terraform Plugin SDKv2 connector.
func IsSDKv2Resource(terraformResourceType string) bool {
	_, ok := loadTerraformResourceRouting().sdkV2Resources[terraformResourceType]
	return ok
}

// HasFrameworkResources reports whether any resources are routed through
// Upjet's in-process Terraform Plugin Framework connector.
func HasFrameworkResources() bool {
	return len(terraformPluginFrameworkIncludeList) != 0
}

func terraformResourceNamesFromSchema() []string {
	return slices.Clone(loadTerraformResourceRouting().names)
}

func parseTerraformResourceNamesFromSchema() []string {
	var schema terraformProviderSchema
	if err := json.Unmarshal([]byte(providerSchema), &schema); err != nil {
		panic(err)
	}

	var resources []string
	for _, provider := range schema.ProviderSchemas {
		for name := range provider.ResourceSchemas {
			resources = append(resources, name)
		}
	}
	sort.Strings(resources)
	return resources
}

func exactResourceRegex(resource string) string {
	return "^" + regexp.QuoteMeta(resource) + "$"
}

func resourceMatches(resource string, regexes []string) bool {
	for _, pattern := range regexes {
		if regexp.MustCompile(pattern).MatchString(resource) {
			return true
		}
	}
	return false
}
