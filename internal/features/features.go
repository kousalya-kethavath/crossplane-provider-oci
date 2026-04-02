/*
 Copyright 2022 Upbound Inc
*/

package features

import "github.com/crossplane/crossplane-runtime/v2/pkg/feature"

// Feature flags.
const (
	// EnableAlphaExternalSecretStores enables alpha support for
	// External Secret Stores. See the below design for more details.
	// https://github.com/crossplane/crossplane/blob/390ddd/design/design-doc-external-secret-stores.md
	EnableAlphaExternalSecretStores feature.Flag = "EnableAlphaExternalSecretStores"

	// EnableAlphaChangeLogs enables alpha support for capturing change logs
	// during reconciliation.
	EnableAlphaChangeLogs feature.Flag = "EnableAlphaChangeLogs"

	// EnableBetaManagementPolicies enables beta support for
	// Management Policies. See the below design for more details.
	// https://github.com/crossplane/crossplane/blob/main/design/design-doc-management-policies.md
	EnableBetaManagementPolicies feature.Flag = "EnableBetaManagementPolicies"
)
