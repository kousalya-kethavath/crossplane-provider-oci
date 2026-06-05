# Terraform OCI Provider Support for Workload Identity Federation

## Goal of Review

Add support in `terraform-provider-oci` for a generic non-OKE Kubernetes workload identity federation authentication mode.

Customers running `crossplane-provider-oci` on non-OKE Kubernetes clusters, including OMK and self-managed clusters, need a supported way to authenticate to OCI using the provider pod's Kubernetes service account identity.

Requested auth mode:

```hcl
auth = "WorkloadIdentityFederation"
```

This mode should be separate from the existing OKE-specific workload identity auth path.

OCI Go SDK already provides the token-exchange building blocks needed for this flow. The Terraform provider should use the OCI Go SDK workload identity federation token-exchange support and act as an adapter from Terraform provider configuration to OCI SDK `common.ConfigurationProvider`.

Once this support is available in an official `terraform-provider-oci` release, `crossplane-provider-oci` can consume that release directly instead of building and packaging a patched Terraform provider binary.

## Current Gap

| Area | Current State | Needed |
| --- | --- | --- |
| Provider auth modes | `terraform-provider-oci` supports API key, instance principal, resource principal, security token, and OKE workload identity. | Add generic `WorkloadIdentityFederation` for non-OKE Kubernetes. |
| OKE workload identity | Existing OKE-specific path is available. | Keep unchanged. Do not reuse it for non-OKE clusters. |
| Generic Kubernetes token exchange | No auth mode accepts a projected Kubernetes service account token from non-OKE clusters. | Accept token-exchange config and create OCI SDK token-exchange provider. |
| Legacy provider path | Auth validation and config provider creation exist in `internal/provider/provider.go`. | Add schema fields and `getConfigProviders()` branch. |
| Plugin-framework path | Auth validation and config provider creation exist in `internal/provider/provider_framework.go`. | Add same fields and `_getConfigProviders()` branch. |
| Crossplane integration | Crossplane delegates OCI SDK client creation to Terraform provider. | Crossplane passes workload identity fields through ProviderConfig; Terraform provider handles SDK auth provider creation. |

## Required Runtime Flow

The intended runtime flow is:

```text
Kubernetes projected service account token
  -> file-backed token issuer
  -> OCI Go SDK auth.TokenExchangeBuilder
  -> auth.TokenExchangeConfigurationProviderFromIssuer(...)
  -> common.ConfigurationProvider
  -> existing OCI SDK client creation flow
```

The Terraform provider should not implement token exchange directly. Token exchange should remain delegated to OCI Go SDK.

## OCI Go SDK Support To Use

The Terraform provider should use OCI Go SDK workload identity federation support, specifically:

```go
auth.TokenIssuer
auth.TokenExchangeBuilder
auth.TokenExchangeConfigurationProviderFromIssuer(...)
auth.InstancePrincipalConfigurationProvider()
```

The provider should implement a file-backed `auth.TokenIssuer` that reads the Kubernetes projected service account token from `workload_identity_token_path`.

Important token behavior:

- `GetToken()` must read the token file each time it is called.
- The token must not be read only once during provider startup.
- The token should be trimmed.
- Empty token file should return a clear error.

This is required so Kubernetes projected service account token rotation works without restarting Terraform or the Crossplane provider pod.

## Proposed Terraform Provider Changes

### Provider Entry Points

There are two provider implementation paths that need to expose the same auth mode and field behavior.

The legacy SDK provider path starts in:

```text
internal/provider/provider.go:93
```

At this location, `descriptions[globalvar.AuthAttrName]` lists supported auth modes. The same file owns the legacy provider schema in `SchemaMap()`, creates the SDK configuration provider in `GetSdkConfigProvider()`, and selects auth-specific configuration providers in `getConfigProviders()`.

The plugin-framework provider path starts in:

```text
internal/provider/provider_framework.go:79
```

In the current upstream file, `Schema()` declares the provider attributes starting at `provider_framework.go:79`. The concrete auth validator list that must add `WorkloadIdentityFederation` is at `provider_framework.go:86`, inside the `globalvar.AuthAttrName` schema block. This path then decodes provider config in `Configure()`, applies environment/default handling in `SetDefaults()`, stores values on `ociPluginProvider`, creates clients in `SetProviderConfig()`, and calls `_GetSdkConfigProvider()` and `_getConfigProviders()` to select the final OCI SDK `common.ConfigurationProvider`.

Before these provider entry points can reference the new auth mode and fields, the shared constants should be added in:

```text
internal/globalvar/constants.go
```

This includes the new auth value, token-exchange field names, and token-exchange auth method values used by both provider paths.

Terraform team should update both paths because users can reach OCI SDK client creation through either the legacy provider implementation or plugin-framework resources/data sources. The new auth mode should behave the same in both paths.

### 1. Add The New Auth Value

Add the new auth value:

```text
WorkloadIdentityFederation
```

This value should be added to shared constants, the shared auth description, legacy `SchemaMap()` validation, and plugin-framework `Schema()` validator. It must be a separate auth mode from the existing OKE workload identity auth mode.

### 2. Update The Legacy Provider Path

In `internal/provider/provider.go`, add the workload identity federation fields to the legacy provider schema and include `WorkloadIdentityFederation` in the auth validator. `GetSdkConfigProvider()` should continue to compose the final SDK configuration provider as it does today, but `getConfigProviders()` should add a new branch for `WorkloadIdentityFederation`.

That new branch should collect the workload identity settings from `schema.ResourceData`, validate them, call the shared helper described below, and append the returned OCI SDK `common.ConfigurationProvider` to the provider list. Existing auth branches should remain unchanged.

Concrete legacy areas to update:

- `internal/globalvar/constants.go`
- `descriptions[globalvar.AuthAttrName]`
- `SchemaMap()` auth validation
- `SchemaMap()` entries for the new token-exchange fields
- `GetSdkConfigProvider()`
- `getConfigProviders()`

### 3. Update The Plugin-Framework Provider Path

In `internal/provider/provider_framework.go`, add the same fields to the plugin-framework model and schema. The framework path should also accept `WorkloadIdentityFederation` in the `globalvar.AuthAttrName` validator at the schema entry point and should pass the same resolved field values into the same shared helper used by the legacy path.

The framework provider currently flows through `Configure()`, `SetDefaults()`, `SetProviderConfig()`, `_GetSdkConfigProvider()`, and `_getConfigProviders()`. The workload identity fields should follow that same lifecycle: decode from Terraform config, apply any supported environment/default lookup, store on `ociPluginProvider`, and use `_getConfigProviders()` to create the OCI SDK token-exchange configuration provider.

Concrete framework areas to update:

- `internal/globalvar/constants.go`
- `ociProviderModel`
- `Schema()` auth validation
- `Schema()` entries for the new token-exchange fields
- `SetDefaults()`
- `Configure()` / provider model value assignment
- `SetProviderConfig()`
- `_GetSdkConfigProvider()`
- `_getConfigProviders()`

Both provider paths should accept the same fields, use the same validation rules, and create the same OCI SDK configuration provider behavior.

### 4. Add A Shared Workload Identity Implementation

Add one shared implementation file:

```text
internal/provider/workload_identity_federation.go
```

This helper should be called by both `getConfigProviders()` and `_getConfigProviders()` so the token exchange behavior does not diverge between provider paths.

Shared responsibilities should include:

- extract workload identity federation settings,
- validate required fields,
- implement file-backed `auth.TokenIssuer`,
- read and trim projected Kubernetes token file,
- map Terraform fields into `auth.TokenExchangeBuilder`,
- call `auth.TokenExchangeConfigurationProviderFromIssuer(...)`,
- return `common.ConfigurationProvider`.

### 5. Audit SDK Client Configuration Propagation

The implementation should verify that the SDK configuration provider returned by the new auth path is consistently used by OCI SDK clients.

This should be treated as an audit first, not a request for broad generated-file changes. Generated service helpers should only be updated if they create or configure clients outside the normal provider `common.ConfigurationProvider` flow.

Implementation review should consider:

| Area | Why It Matters |
| --- | --- |
| `internal/client/provider_clients.go` | Ensures OCI SDK clients are created with the workload identity `common.ConfigurationProvider`. |
| generated service helper files | Some service helpers may create/configure clients outside the direct provider setup path. |
| `oci/provider.go` | Public provider entry point should use the configured SDK provider consistently. |

## Provider Configuration Fields

Add support for the following fields.

| Field | Required When | Purpose |
| --- | --- | --- |
| `auth` | Always | New value: `WorkloadIdentityFederation`. |
| `region` | `WorkloadIdentityFederation` | OCI region for SDK clients. |
| `workload_identity_token_path` | `WorkloadIdentityFederation` | Path to projected Kubernetes service account token. |
| `token_exchange_domain_url` | `WorkloadIdentityFederation` | OCI IAM Identity Domain URL where the Identity Propagation Trust exists. |
| `token_exchange_auth_method` | Optional | Token exchange authorization method. Values: `ClientSecret` or `InstancePrincipal`. Default: `ClientSecret`. |
| `token_exchange_client_id` | `ClientSecret` | Identity Domain OAuth confidential application client ID. This OAuth client is referenced by the Identity Propagation Trust. |
| `token_exchange_client_secret` | `ClientSecret` | Identity Domain OAuth confidential application client secret. Must be sensitive. |
| `token_exchange_requested_token_type` | `WorkloadIdentityFederation` | Requested token type. For the self-managed Kubernetes RPST flow: `urn:oci:token-type:oci-rpst`. |
| `token_exchange_subject_token_type` | Optional | Subject token type for the Kubernetes service account token. Default: `jwt`. |
| `token_exchange_resource_type` | RPST token exchange | Resource type defined by Identity Propagation Trust `impersonatingResource`. In the OCI guide example: `k8sworkload`. |
| `token_exchange_rpst_exp` | Optional | Requested RPST expiration, for example `3600`. |
| `token_exchange_public_key` | Optional / SDK-flow-specific | Optional public key pass-through to OCI Go SDK token exchange builder. Include only when the OCI SDK token-exchange flow requires it. This is not the same as the Identity Propagation Trust `publicCertificate`. |

Clarification: existing top-level `auth = "InstancePrincipal"` remains unchanged and continues to authenticate directly as the OCI compute instance. The new `auth = "WorkloadIdentityFederation"` with `token_exchange_auth_method = "InstancePrincipal"` is different: instance principal is used only to authorize the Kubernetes service account token exchange request, and the resulting SDK config provider represents the federated workload identity.

Validation rules:

| Condition | Expected Validation |
| --- | --- |
| `auth = WorkloadIdentityFederation` and `workload_identity_token_path` is empty | Return clear error. |
| `auth = WorkloadIdentityFederation` and `token_exchange_domain_url` is empty | Return clear error. |
| `auth = WorkloadIdentityFederation` and `region` is empty | Return clear error. |
| `auth = WorkloadIdentityFederation` and `token_exchange_requested_token_type` is empty | Return clear error. |
| `token_exchange_auth_method` is empty | Default to `ClientSecret`. |
| `token_exchange_requested_token_type = urn:oci:token-type:oci-rpst` and `token_exchange_resource_type` is empty | Return clear error. |
| `token_exchange_auth_method = ClientSecret` and client ID or secret is empty | Return clear error. |
| `token_exchange_auth_method = InstancePrincipal` and client ID or secret is provided | Return clear error. |
| token file exists but is empty or whitespace only | Return clear error before token exchange. |

Field mapping from the OCI self-managed Kubernetes workload identity guide:

| Provider Field | Source / Meaning |
| --- | --- |
| `token_exchange_domain_url` | Identity Domain URL for the domain containing the Identity Propagation Trust. |
| `token_exchange_client_id` | Identity Domain application OAuth client ID used in the trust configuration. |
| `token_exchange_client_secret` | Secret for the Identity Domain OAuth client. |
| `token_exchange_requested_token_type` | `urn:oci:token-type:oci-rpst` for the KSAT-to-RPST flow. |
| `token_exchange_subject_token_type` | Kubernetes service account token is a JWT; use `jwt`, matching the guide's SDK builder example. |
| `token_exchange_resource_type` | `RES_TYPE` from the guide; resource type defined in the trust config. The example uses `impersonatingResource = "k8sworkload"`. |
| `token_exchange_public_key` | Optional SDK builder value shown in the instance-principal example as `PublicKey`; not the same as trust setup `publicCertificate`. |

Security handling:

- `token_exchange_client_secret` must be marked `Sensitive` in both provider schemas.
- `token_exchange_client_secret` must not be logged.
- validation errors and diagnostics should name the field but should not include the secret value.

## OAuth Client Secret Mode

Use this mode when the cluster cannot use OCI instance principals.

The provider enum value should be `ClientSecret`. This is the OAuth confidential-client mode from an IAM setup perspective, but `ClientSecret` is clearer in provider configuration because it maps directly to `TokenExchangeBuilder.ClientId` and `TokenExchangeBuilder.ClientSecret`.

Terraform config shape:

```hcl
provider "oci" {
  auth   = "WorkloadIdentityFederation"
  region = "us-ashburn-1"

  workload_identity_token_path        = "/var/run/secrets/tokens/oci"
  token_exchange_domain_url           = "https://<identity-domain-url>"
  token_exchange_auth_method          = "ClientSecret"
  token_exchange_client_id            = "<client-id>"
  token_exchange_client_secret        = "<client-secret>"
  token_exchange_requested_token_type = "urn:oci:token-type:oci-rpst"
  token_exchange_subject_token_type   = "jwt"
  token_exchange_resource_type        = "k8sworkload"
  token_exchange_rpst_exp             = "3600"
}
```

Runtime behavior:

```text
projected Kubernetes token
  -> file-backed TokenIssuer
  -> TokenExchangeBuilder with ClientId and ClientSecret
  -> TokenExchangeConfigurationProviderFromIssuer(...)
```

OCI IAM setup is external to Terraform provider initialization. Users must configure:

- Identity Propagation Trust,
- Identity Domain confidential application / OAuth client,
- OCI policy for the federated workload principal.

For this mode, `TokenExchangeBuilder.ClientId` and `TokenExchangeBuilder.ClientSecret` should be populated from `token_exchange_client_id` and `token_exchange_client_secret`.

## Instance Principal Mode

Use this mode when the Kubernetes worker nodes run on OCI Compute and the provider pod can use instance principal metadata.

Terraform config shape:

```hcl
provider "oci" {
  auth   = "WorkloadIdentityFederation"
  region = "us-ashburn-1"

  workload_identity_token_path        = "/var/run/secrets/tokens/oci"
  token_exchange_domain_url           = "https://<identity-domain-url>"
  token_exchange_auth_method          = "InstancePrincipal"
  token_exchange_requested_token_type = "urn:oci:token-type:oci-rpst"
  token_exchange_subject_token_type   = "jwt"
  token_exchange_resource_type        = "k8sworkload"
  token_exchange_rpst_exp             = "3600"
}
```

Runtime behavior:

```text
projected Kubernetes token
  -> file-backed TokenIssuer
  -> auth.InstancePrincipalConfigurationProvider()
  -> TokenExchangeBuilder.InstancePrincipalProvider
  -> TokenExchangeConfigurationProviderFromIssuer(...)
```

The provider should reject `token_exchange_client_id` and `token_exchange_client_secret` when `token_exchange_auth_method = "InstancePrincipal"`.

For this mode, the provider should call `auth.InstancePrincipalConfigurationProvider()` and set `TokenExchangeBuilder.InstancePrincipalProvider`.

OCI IAM setup is external to Terraform provider initialization. Users must configure:

- Identity Propagation Trust,
- dynamic group for Kubernetes worker nodes,
- policy allowing that dynamic group to call token exchange:

```text
Allow dynamic-group <worker-node-dynamic-group> to {GET_RPST} in tenancy
```

## Crossplane Consumption

`crossplane-provider-oci` will not perform token exchange directly.

Crossplane will:

- mount a projected Kubernetes service account token into the provider pod,
- read ProviderConfig credentials from a Kubernetes Secret,
- pass workload identity fields to Terraform provider configuration,
- rely on `terraform-provider-oci` to create the OCI SDK `common.ConfigurationProvider`.

Expected Crossplane OAuth credential shape:

```json
{
  "auth": "WorkloadIdentityFederation",
  "region": "us-ashburn-1",
  "workload_identity_token_path": "/var/run/secrets/tokens/oci",
  "token_exchange_domain_url": "https://<identity-domain-url>",
  "token_exchange_auth_method": "ClientSecret",
  "token_exchange_client_id": "<client-id>",
  "token_exchange_client_secret": "<client-secret>",
  "token_exchange_requested_token_type": "urn:oci:token-type:oci-rpst",
  "token_exchange_subject_token_type": "jwt",
  "token_exchange_resource_type": "k8sworkload",
  "token_exchange_rpst_exp": "3600"
}
```

Expected Crossplane instance-principal credential shape:

```json
{
  "auth": "WorkloadIdentityFederation",
  "region": "us-ashburn-1",
  "workload_identity_token_path": "/var/run/secrets/tokens/oci",
  "token_exchange_domain_url": "https://<identity-domain-url>",
  "token_exchange_auth_method": "InstancePrincipal",
  "token_exchange_requested_token_type": "urn:oci:token-type:oci-rpst",
  "token_exchange_subject_token_type": "jwt",
  "token_exchange_resource_type": "k8sworkload",
  "token_exchange_rpst_exp": "3600"
}
```

`token_exchange_public_key` is optional and intentionally omitted from the primary examples. It should only be configured when the OCI Go SDK token-exchange flow being used requires `TokenExchangeBuilder.PublicKey`.

After this support is released upstream, Crossplane can remove the patched Terraform provider packaging and consume the official Terraform provider version containing `WorkloadIdentityFederation`.

## IAM Setup Boundary

The Terraform provider should not create IAM setup. These remain user/admin setup tasks:

- Kubernetes issuer/public key discovery,
- Identity Propagation Trust creation,
- OAuth confidential application creation for `ClientSecret`,
- dynamic group and `{GET_RPST}` policy for `InstancePrincipal`,
- resource authorization policy for the federated workload principal.

`token_exchange_resource_type` is not discovered by the provider. It must match the Identity Propagation Trust `impersonatingResource`. In the OCI self-managed Kubernetes guide example, this value is:

```text
k8sworkload
```

## Acceptance Criteria

- `terraform-provider-oci` accepts `auth = "WorkloadIdentityFederation"`.
- Both legacy and plugin-framework provider paths support the new auth mode.
- Both provider paths accept the same token-exchange configuration fields.
- Missing required fields return clear validation errors.
- `token_exchange_client_secret` is treated as sensitive and is not logged or echoed in diagnostics.
- The provider uses OCI Go SDK token-exchange support.
- The provider calls `auth.TokenExchangeConfigurationProviderFromIssuer(...)`.
- The Kubernetes projected service account token is treated as the subject token.
- The file-backed token issuer rereads `workload_identity_token_path` on each token request.
- Empty token file returns a clear error.
- `ClientSecret` mode maps `token_exchange_client_id` and `token_exchange_client_secret` to `TokenExchangeBuilder.ClientId` and `TokenExchangeBuilder.ClientSecret`.
- `InstancePrincipal` mode calls `auth.InstancePrincipalConfigurationProvider()` and maps it to `TokenExchangeBuilder.InstancePrincipalProvider`.
- `InstancePrincipal` mode rejects client ID and client secret fields.
- Existing auth modes continue to work unchanged.
- Existing OKE workload identity behavior remains unchanged.

## Testing Expectations

Terraform provider tests should verify:

- `WorkloadIdentityFederation` is accepted in both provider paths.
- Required field validation works.
- Token file reading trims whitespace.
- Token refresh behavior is unit-tested by changing the temp token file between two `GetToken()` calls and verifying the second call returns the updated token.
- Empty token file fails clearly.
- OCI SDK token-exchange configuration provider is used.
- `ClientSecret` mode populates SDK client ID and secret fields.
- `InstancePrincipal` mode populates SDK instance principal provider. This should be tested with an injectable or mockable helper so unit tests do not require live OCI instance metadata.
- secret handling tests verify that `token_exchange_client_secret` is marked sensitive and does not appear in validation diagnostics.
- Existing OKE workload identity behavior is not regressed.

Crossplane Provider for OCI will verify:

- provider pod starts without OCI user API key credentials,
- projected Kubernetes service account token is mounted at the configured path,
- ProviderConfig with workload identity federation auth is accepted,
- at least one OCI managed resource can be observed or reconciled,
- token rotation does not require restarting the provider pod.

## Success Criteria

- `terraform-provider-oci` exposes official generic workload identity federation auth support for non-OKE Kubernetes clusters.
- `crossplane-provider-oci` can consume an official `terraform-provider-oci` release instead of maintaining a patched provider binary.
- Crossplane users can avoid long-lived OCI user API keys in Kubernetes Secrets for this auth path.

## Open Questions

- Should `token_exchange_resource_type` default to `k8sworkload`, or remain explicit because it depends on the Identity Propagation Trust?
- Should provider schema fields also support `TF_VAR_*` and `OCI_*` environment variable fallbacks?
- Is `internal/provider/workload_identity_federation.go` the preferred shared helper location?
