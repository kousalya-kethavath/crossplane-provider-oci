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
	frameworkprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	tfoci "github.com/oracle/terraform-provider-oci/oci"
)

func terraformSDKProvider(_ []string) *schema.Provider {
	// The minimum upstream contract exposes a full, isolated SDKv2 provider.
	// Crossplane still scopes controller registration and connector routing to
	// the service runtime; only the embedded Terraform schema remains complete.
	return tfoci.Provider()
}

func terraformFrameworkProvider() frameworkprovider.Provider {
	return tfoci.New()
}
