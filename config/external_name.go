/*
 * Copyright (c) 2023 Oracle and/or its affiliates
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
	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/oracle/provider-oci/config/common"
)

// ExternalNameConfigs contains all external name configurations for this
// provider.
var ExternalNameConfigs = map[string]config.ExternalName{
	// Add Manual External Name Configurations that are not IdentifierFromProvider here
}

// AutoExternalNameConfiguration provides automatic external name configuration
// for resources that don't have explicit configuration in ExternalNameConfigs.
// This ensures all discovered resources can be properly managed.
func AutoExternalNameConfiguration() config.ResourceOption {
	return func(r *config.Resource) {
		// Only apply if not already configured
		if r.ExternalName.DisableNameInitializer == false {
			// Check if this resource has explicit configuration
			if _, ok := ExternalNameConfigs[r.Name]; !ok {
				// Use IdentifierFromProvider as default for all OCI resources
				// This is the most common pattern in OCI
				r.ExternalName = config.IdentifierFromProvider
				r.Version = "v1alpha1"
			}
		}
	}
}

// ExternalNameConfigurations applies all external name configs listed in the
// table ExternalNameConfigs and sets the version of those resources to v1beta1
// assuming they will be tested.
func ExternalNameConfigurations() config.ResourceOption {
	return func(r *config.Resource) {
		if e, ok := ExternalNameConfigs[r.Name]; ok {
			r.ExternalName = e
			r.Version = common.VersionAlpha1
		}
	}
}

// ExternalNameConfigured returns the list of all resources whose external name
// is configured manually.
func ExternalNameConfigured() []string {
	l := make([]string, len(ExternalNameConfigs))
	i := 0
	for name := range ExternalNameConfigs {
		// $ is added to match the exact string since the format is regex.
		l[i] = name + "$"
		i++
	}
	return l
}
