# Workload Identity Federation Support for Non-OKE Kubernetes

## Summary

`crossplane-provider-oci` now supports a generic workload identity federation path for Kubernetes clusters that are not using OKE workload identity. The provider can run in a self-managed or non-OKE Kubernetes cluster, read a projected Kubernetes service account token, and use OCI IAM token exchange to get short-lived OCI credentials for resource reconciliation.

The Crossplane provider still delegates OCI resource operations to `terraform-provider-oci`. Because the upstream Terraform OCI provider does not currently expose this generic token-exchange auth mode, this repository builds and packages a locally patched `terraform-provider-oci` binary.

Supported token exchange authorization methods:

- `ClientSecret`: uses an OCI IAM identity domain confidential OAuth client ID and client secret.
- `InstancePrincipal`: uses OCI instance principal credentials from the Kubernetes worker node to authorize the token exchange request.

Both methods use the same final workload identity flow:

```text
Kubernetes service account token
  -> OCI IAM token exchange
  -> RPST / OCI security token
  -> OCI Go SDK clients inside terraform-provider-oci
  -> OCI resource APIs
```

## Why This Was Added

Before this change, the usual non-OKE Crossplane setup required long-lived OCI user API key material in a Kubernetes Secret. That model has several drawbacks:

- It authenticates as an OCI user instead of the Kubernetes workload.
- It requires distributing and rotating private key material in clusters.
- It does not naturally scope access to the provider service account, namespace, or workload.
- OCI audit records do not clearly identify the Kubernetes workload as the acting principal.

The workload identity model lets OCI IAM trust a Kubernetes service account token issuer. The Crossplane provider pod uses a projected token from its service account. OCI IAM validates that token through an identity propagation trust and returns short-lived OCI credentials.

## What Was Added

### Crossplane Credential Pass-Through

File:

```text
internal/clients/oci.go
```

The provider now passes additional credential keys from the Crossplane `ProviderConfig` Secret into Terraform provider configuration:

```text
workload_identity_token_path
token_exchange_domain_url
token_exchange_auth_method
token_exchange_client_id
token_exchange_client_secret
token_exchange_requested_token_type
token_exchange_subject_token_type
token_exchange_resource_type
token_exchange_rpst_exp
token_exchange_public_key
```

Crossplane does not perform the token exchange itself. It passes these values to the patched Terraform provider, which creates the OCI SDK configuration provider.

### Patched Terraform OCI Provider

File:

```text
hack/oci-provider-workload-identity.patch
```

The patch adds the following Terraform OCI provider auth mode:

```text
WorkloadIdentityFederation
```

The patch updates both Terraform provider paths:

- legacy plugin SDK path: `internal/provider/provider.go`
- plugin framework path: `internal/provider/provider_framework.go`

Both paths call shared code added by the patch:

```text
internal/provider/workload_identity_federation.go
```

That shared helper:

- reads the Kubernetes service account token from `workload_identity_token_path`,
- errors if the token file is empty,
- validates required token-exchange settings,
- builds an OCI Go SDK `auth.TokenExchangeBuilder`,
- calls `auth.TokenExchangeConfigurationProviderFromIssuer(...)`,
- returns an OCI SDK `common.ConfigurationProvider` used by Terraform OCI SDK clients.

### OAuth and Instance Principal Modes

The patch adds:

```text
token_exchange_auth_method
```

Valid values:

```text
ClientSecret
InstancePrincipal
```

`ClientSecret` is the default for backward compatibility with the first version of the patch.

In `ClientSecret` mode, the patched Terraform provider sets:

```go
TokenExchangeBuilder.ClientId
TokenExchangeBuilder.ClientSecret
```

In `InstancePrincipal` mode, the patched Terraform provider calls:

```go
auth.InstancePrincipalConfigurationProvider()
```

and sets:

```go
TokenExchangeBuilder.InstancePrincipalProvider
```

This matches the instance-principal token exchange flow described in the OCI workload identity document for self-managed Kubernetes clusters.

### Patched Provider Build and Image Packaging

Files:

```text
Makefile
hack/validate-oci-provider-patch.sh
hack/patch-tf-provider.sh
hack/build-patched-oci-provider.sh
cluster/images/provider-oci/Dockerfile
cluster/images/provider-oci/Makefile
cluster/images/provider-oci/terraformrc.hcl
```

The provider image now packages a locally built, patched `terraform-provider-oci` binary into the Terraform plugin mirror. `terraformrc.hcl` forces Terraform to use the filesystem mirror for `registry.terraform.io/oracle/oci` instead of downloading the official upstream provider binary.

The Terraform provider version is pinned to:

```text
8.12.0
```

## Configuration

### Shared Kubernetes Runtime Configuration

Both OAuth and instance-principal modes require the provider pod to mount a projected service account token. The example uses:

```yaml
apiVersion: pkg.crossplane.io/v1beta1
kind: DeploymentRuntimeConfig
metadata:
  name: oci-workload-identity-runtime
spec:
  deploymentTemplate:
    spec:
      selector: {}
      template:
        spec:
          containers:
            - name: package-runtime
              volumeMounts:
                - name: oci-workload-identity-token
                  mountPath: /var/run/secrets/tokens
                  readOnly: true
          volumes:
            - name: oci-workload-identity-token
              projected:
                sources:
                  - serviceAccountToken:
                      path: oci
                      audience: oci
                      expirationSeconds: 3600
```

The matching credential value is:

```json
"workload_identity_token_path": "/var/run/secrets/tokens/oci"
```

### OAuth Client Secret Mode

Use this mode when the cluster does not run on OCI compute, or when instance principals are not available.

Credential JSON:

```json
{
  "auth": "WorkloadIdentityFederation",
  "region": "us-ashburn-1",
  "workload_identity_token_path": "/var/run/secrets/tokens/oci",
  "token_exchange_domain_url": "https://identity.oraclecloud.com/...",
  "token_exchange_auth_method": "ClientSecret",
  "token_exchange_client_id": "replace-with-client-id",
  "token_exchange_client_secret": "replace-with-client-secret",
  "token_exchange_requested_token_type": "urn:oci:token-type:oci-rpst",
  "token_exchange_subject_token_type": "jwt",
  "token_exchange_resource_type": "replace-with-resource-type",
  "token_exchange_rpst_exp": "3600"
}
```

Required OCI IAM setup:

- Identity propagation trust for the Kubernetes token issuer.
- Identity domain confidential application / OAuth client.
- Client ID and client secret stored in a Kubernetes Secret.
- OCI policy granting the federated workload principal access to required OCI resources.

### Instance Principal Mode

Use this mode when the Crossplane provider pod runs on OCI compute worker nodes. This avoids storing an OAuth client secret in Kubernetes.

Credential JSON:

```json
{
  "auth": "WorkloadIdentityFederation",
  "region": "us-ashburn-1",
  "workload_identity_token_path": "/var/run/secrets/tokens/oci",
  "token_exchange_domain_url": "https://identity.oraclecloud.com/...",
  "token_exchange_auth_method": "InstancePrincipal",
  "token_exchange_requested_token_type": "urn:oci:token-type:oci-rpst",
  "token_exchange_subject_token_type": "jwt",
  "token_exchange_resource_type": "replace-with-resource-type",
  "token_exchange_rpst_exp": "3600"
}
```

Do not set these fields in instance-principal mode:

```text
token_exchange_client_id
token_exchange_client_secret
```

Required OCI IAM setup:

- Identity propagation trust for the Kubernetes token issuer.
- Worker nodes in an OCI dynamic group.
- Policy allowing those nodes to call token exchange:

```text
Allow dynamic-group <worker-node-dynamic-group> to {GET_RPST} in tenancy
```

- OCI policy granting the federated workload principal access to required OCI resources.

If the dynamic group is not in the default identity domain, prefix the dynamic group with the identity domain name in the policy.

## OCI IAM Setup Overview

For both modes, OCI IAM must trust the Kubernetes token issuer.

At a high level, configure:

1. Kubernetes service account token projection for the provider pod.
2. OCI IAM identity propagation trust:
   - issuer: Kubernetes service account token issuer,
   - public certificate or JWKS for token validation,
   - subject type/resource configuration,
   - claim propagation such as issuer or service account subject claims,
   - active trust.
3. Token exchange authorization:
   - OAuth confidential client for `ClientSecret`, or
   - worker-node dynamic group policy for `InstancePrincipal`.
4. Resource authorization policy for the federated workload principal.

The workload identity implementation only obtains OCI credentials. It does not automatically grant permissions to create or manage OCI resources. Those permissions still come from IAM policies.

## Which Mode To Use

Prefer `InstancePrincipal` when:

- the Kubernetes worker nodes run on OCI compute,
- instance principal metadata service is available to the provider pod runtime,
- you want to avoid storing OAuth client secrets in Kubernetes.

Use `ClientSecret` when:

- the cluster is outside OCI compute,
- the provider pod cannot use instance principals,
- your environment standardizes on identity domain OAuth clients for token exchange.

## How To Test

### 1. Run Crossplane Unit Tests

```bash
go test ./internal/clients
```

This verifies that Crossplane passes the workload identity fields into Terraform provider configuration and that the patch contains the workload identity hooks.

### 2. Validate the Terraform Provider Patch

```bash
make validate-oci-provider-patch
```

This clones `terraform-provider-oci` at version `8.12.0` and verifies that `hack/oci-provider-workload-identity.patch` applies cleanly.

### 3. Build the Patched Terraform Provider

For a fast local compile check:

```bash
make build-patched-oci-provider PATCHED_TERRAFORM_PROVIDER_PLATFORMS=linux_amd64
```

Expected output:

```text
_output/terraform-provider/linux_amd64/terraform-provider-oci_v8.12.0
```

This catches compile errors in the patched Terraform provider code, including the shared workload identity helper.

### 4. Build the Provider Image

```bash
make do.build.images
```

The image build should copy the patched Terraform provider binary into the filesystem mirror used by Terraform.

### 5. Deploy with OAuth Mode

1. Configure OCI IAM identity propagation trust.
2. Create an identity domain OAuth confidential client.
3. Store the client ID and client secret in the `ProviderConfig` credentials Secret.
4. Apply the OAuth example from:

```text
examples/providerconfig/workload-identity.yaml
```

Use the `oci-workload-identity-oauth` `ProviderConfig`.

5. Create a low-risk managed resource in a test compartment.
6. Confirm the resource becomes ready.
7. Check provider logs for token exchange or authorization errors.

### 6. Deploy with Instance Principal Mode

1. Configure OCI IAM identity propagation trust.
2. Put worker nodes in an OCI dynamic group.
3. Add the `GET_RPST` policy for that dynamic group.
4. Apply the instance-principal example from:

```text
examples/providerconfig/workload-identity.yaml
```

Use the `oci-workload-identity-instance-principal` `ProviderConfig`.

5. Create a low-risk managed resource in a test compartment.
6. Confirm the resource becomes ready.
7. Check provider logs for token exchange, instance-principal metadata, or policy errors.

### 7. Verify the Projected Token File

The provider pod must see a non-empty token file:

```bash
kubectl -n crossplane-system exec deploy/<provider-deployment> -- \
  test -s /var/run/secrets/tokens/oci
```

If the file is missing or empty, the patched provider should fail before token exchange with an error indicating that `workload_identity_token_path` is empty or unreadable.

### 8. Negative Tests

Run these failure checks before considering the feature complete:

- Missing `workload_identity_token_path`.
- Empty projected token file.
- Invalid `token_exchange_domain_url`.
- Missing `token_exchange_client_id` or `token_exchange_client_secret` in `ClientSecret` mode.
- Setting client ID or client secret in `InstancePrincipal` mode.
- Worker nodes not in a dynamic group for `InstancePrincipal` mode.
- Missing `GET_RPST` policy for instance-principal token exchange.
- Missing resource authorization policy for the federated workload principal.
- Wrong `token_exchange_resource_type`.
- Wrong Kubernetes token issuer or trust public key.

## Troubleshooting

### Token file errors

Check the runtime config and mounted path:

```bash
kubectl -n crossplane-system describe pod <provider-pod>
kubectl -n crossplane-system exec <provider-pod> -- ls -l /var/run/secrets/tokens
```

### Token exchange authorization errors

For `ClientSecret`, verify:

- domain URL,
- client ID,
- client secret,
- identity propagation trust references the OAuth client where required.

For `InstancePrincipal`, verify:

- provider pod is scheduled on OCI compute worker nodes,
- instance principal metadata service is available,
- worker nodes match the dynamic group rule,
- dynamic group has `{GET_RPST}` permission.

### Resource authorization errors

Successful token exchange does not mean the workload can manage resources. Verify OCI policies for the federated workload principal, including any conditions based on token claims such as issuer, subject, identity domain, or propagated claims.

## Implementation Boundary

This change intentionally keeps token exchange inside the patched Terraform OCI provider because that is where OCI SDK clients are created for Terraform-backed managed resources.

Crossplane's responsibility is:

- resolve `ProviderConfig`,
- read the credentials Secret,
- pass workload identity fields to Terraform,
- package a provider image that contains the patched Terraform OCI provider binary.

Terraform OCI provider's responsibility is:

- validate `auth = WorkloadIdentityFederation`,
- read the projected Kubernetes token,
- choose `ClientSecret` or `InstancePrincipal` authorization for token exchange,
- create the OCI SDK token-exchange configuration provider,
- configure OCI SDK clients.

## Current Status

Validated locally:

```bash
go test ./internal/clients
make validate-oci-provider-patch
make build-patched-oci-provider PATCHED_TERRAFORM_PROVIDER_PLATFORMS=linux_amd64
```

The local build produces:

```text
_output/terraform-provider/linux_amd64/terraform-provider-oci_v8.12.0
```

End-to-end validation still requires a configured Kubernetes cluster, OCI IAM identity propagation trust, and OCI policies for either OAuth or instance-principal token exchange.
