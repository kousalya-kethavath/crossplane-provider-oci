/*
 * Copyright (c) 2022, 2023 Oracle and/or its affiliates
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

package identity

import "github.com/crossplane/upjet/v2/pkg/config"

// Configure configures individual resources by adding custom ResourceConfigurators.
func Configure(p *config.Provider) {

	p.AddResourceConfigurator("oci_identity_authentication_policy", func(r *config.Resource) {
		r.References["compartment_id"] = config.Reference{
			TerraformName: "oci_identity_compartment",
		}
	})

	p.AddResourceConfigurator("oci_identity_tag_namespace", func(r *config.Resource) {
		r.References["compartment_id"] = config.Reference{
			TerraformName: "oci_identity_compartment",
		}
	})
	p.AddResourceConfigurator("oci_identity_policy", func(r *config.Resource) {
		r.References["compartment_id"] = config.Reference{
			TerraformName: "oci_identity_compartment",
		}
	})

	p.AddResourceConfigurator("oci_identity_tag", func(r *config.Resource) {
		r.References["tag_namespace_id"] = config.Reference{
			TerraformName: "oci_identity_tag_namespace",
		}
	})

	p.AddResourceConfigurator("oci_identity_identity_provider", func(r *config.Resource) {
		r.References["compartment_id"] = config.Reference{
			TerraformName: "oci_identity_compartment",
		}
	})
	p.AddResourceConfigurator("oci_identity_api_key", func(r *config.Resource) {
		r.References["user_id"] = config.Reference{
			TerraformName: "oci_identity_user",
		}
	})

	p.AddResourceConfigurator("oci_identity_auth_token", func(r *config.Resource) {
		r.References["user_id"] = config.Reference{
			TerraformName: "oci_identity_user",
		}
	})

	p.AddResourceConfigurator("oci_identity_customer_secret_key", func(r *config.Resource) {
		r.References["user_id"] = config.Reference{
			TerraformName: "oci_identity_user",
		}
	})

	p.AddResourceConfigurator("oci_identity_db_credential", func(r *config.Resource) {
		r.References["user_id"] = config.Reference{
			TerraformName: "oci_identity_user",
		}
	})
	p.AddResourceConfigurator("oci_identity_domain", func(r *config.Resource) {
		r.References["compartment_id"] = config.Reference{
			TerraformName: "oci_identity_compartment",
		}
		/*
			r.References["admin_user_name"] = config.Reference{
				TerraformName: "oci_identity_user",
				// We need to extract the name from the user ID
				// The user ID is in the format `ocid1.user.oc1..<region>..<id>`
				// We need to get the name from the ID
				// This can be achieved by using the `config.IdentifierFromProvider` for the `admin_user_name`
				// However, this is not directly possible as it's not an ID, it's a name
				// So, we need to use a different approach, possibly using a custom extractor or a different reference type
			}
		*/
	})

	p.AddResourceConfigurator("oci_identity_domain_replication_to_region", func(r *config.Resource) {
		r.References["domain_id"] = config.Reference{
			TerraformName: "oci_identity_domain",
		}
	})

	p.AddResourceConfigurator("oci_identity_dynamic_group", func(r *config.Resource) {
		r.References["compartment_id"] = config.Reference{
			TerraformName: "oci_identity_compartment",
		}
	})

	p.AddResourceConfigurator("oci_identity_idp_group_mapping", func(r *config.Resource) {
		r.References["identity_provider_id"] = config.Reference{
			TerraformName: "oci_identity_identity_provider",
		}
		r.References["group_id"] = config.Reference{
			TerraformName: "oci_identity_group",
		}
	})
	p.AddResourceConfigurator("oci_identity_import_standard_tags_management", func(r *config.Resource) {
		r.References["compartment_id"] = config.Reference{
			TerraformName: "oci_identity_compartment",
		}
		/*
			r.References["standard_tag_namespace_name"] = config.Reference{
				TerraformName: "oci_identity_tag_namespace",
				// We need to extract the name from the tag namespace ID
				// The tag namespace ID is in the format `ocid1.tagnamespace.oc1..<region>..<id>`
				// We need to get the name from the ID
				// This can be achieved by using the `config.IdentifierFromProvider` for the `standard_tag_namespace_name`
				// However, this is not directly possible as it's not an ID, it's a name
				// So, we need to use a different approach, possibly using a custom extractor or a different reference type
			}
		*/
	})

	p.AddResourceConfigurator("oci_identity_network_source", func(r *config.Resource) {
		r.References["compartment_id"] = config.Reference{
			TerraformName: "oci_identity_compartment",
		}
	})

	p.AddResourceConfigurator("oci_identity_smtp_credential", func(r *config.Resource) {
		r.References["user_id"] = config.Reference{
			TerraformName: "oci_identity_user",
		}
	})

	p.AddResourceConfigurator("oci_identity_tag_default", func(r *config.Resource) {
		r.References["compartment_id"] = config.Reference{
			TerraformName: "oci_identity_compartment",
		}
		r.References["tag_definition_id"] = config.Reference{
			TerraformName: "oci_identity_tag",
		}
	})

	p.AddResourceConfigurator("oci_identity_ui_password", func(r *config.Resource) {
		r.References["user_id"] = config.Reference{
			TerraformName: "oci_identity_user",
		}
	})

	p.AddResourceConfigurator("oci_identity_user_capabilities_management", func(r *config.Resource) {
		r.References["user_id"] = config.Reference{
			TerraformName: "oci_identity_user",
		}
	})

	p.AddResourceConfigurator("oci_identity_user_group_membership", func(r *config.Resource) {
		r.References["user_id"] = config.Reference{
			TerraformName: "oci_identity_user",
		}
		r.References["group_id"] = config.Reference{
			TerraformName: "oci_identity_group",
		}
	})
}
