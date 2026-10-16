# Namespaced ServiceAccount authentication on OKE

Use `credentials.source: ServiceAccount` on a namespaced
`ProviderConfig.oci.m.upbound.io` to select an OCI workload identity for a tenant.
The shared provider requests a short-lived token for that ServiceAccount and
uses it through the embedded Terraform provider's OKE authentication path.
Changing the provider Deployment's ServiceAccount is not required per tenant.

This source is supported only on namespaced ProviderConfigs. ClusterProviderConfig
and legacy cluster-scoped ProviderConfig authentication retain their existing
behavior. A namespaced managed resource must explicitly select
`providerConfigRef.kind: ProviderConfig`; its default is ClusterProviderConfig.

## Prerequisites

- An OKE Enhanced cluster and OCI IAM policies for each selected workload identity.
- Matching provider CRDs and service controller images containing this feature.
- The usual OKE runtime configuration, including `OCI_RESOURCE_PRINCIPAL_VERSION`,
  `OCI_RESOURCE_PRINCIPAL_REGION`, the Kubernetes service host and trusted CA.
  `spec.region` selects OCI API client endpoints; the SDK still requires the
  runtime OKE environment to initialize the exchange.
- Namespace-local permission for the actual reconciling provider ServiceAccount
  to read the selected tenant ServiceAccount and create its tokens.

Apply grants only to provider runtimes that need them, including each relevant
service subprovider. The family/config controller does not perform every managed
resource reconciliation. No cluster-wide token permission is installed by default.

## Configure the tenant identity

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: oci-access
  namespace: tenant-a
---
apiVersion: oci.m.upbound.io/v1beta1
kind: ProviderConfig
metadata:
  name: tenant-oci
  namespace: tenant-a
spec:
  region: us-ashburn-1
  credentials:
    source: ServiceAccount
    serviceAccountRef:
      name: oci-access
```

The ServiceAccount must exist in the ProviderConfig's namespace. The reference
accepts only a name. The new source requires `spec.region` and cannot also select
a Secret, environment variable or credentials file. Existing sources continue
to configure their region through their credential data.

The [two-tenant example](../examples/namespaced/providerconfig/serviceaccount.yaml)
uses the same account name in two different namespaces. Each namespaced
ProviderConfig resolves its own account; it does not borrow another namespace's
account or fall back to the provider pod's OCI identity.
The custom Terraform authentication path also excludes ambient credentials from
OCI configuration files and environment variables when tenant token exchange fails.

## Grant token permission explicitly

Replace `PROVIDER_RUNTIME_SERVICEACCOUNT` with the effective provider pod's
Kubernetes ServiceAccount name and adjust its namespace. Apply an equivalent
RoleBinding for every opted-in reconciling runtime and tenant namespace.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: crossplane-oci-token-request
  namespace: tenant-a
rules:
  - apiGroups: [""]
    resources: ["serviceaccounts"]
    resourceNames: ["oci-access"]
    verbs: ["get"]
  - apiGroups: [""]
    resources: ["serviceaccounts/token"]
    resourceNames: ["oci-access"]
    verbs: ["create"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: crossplane-oci-token-request
  namespace: tenant-a
subjects:
  - kind: ServiceAccount
    name: PROVIDER_RUNTIME_SERVICEACCOUNT
    namespace: crossplane-system
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: crossplane-oci-token-request
```

The provider uses uncached ServiceAccount reads, so these grants do not need
cluster-wide ServiceAccount list/watch access. Control who may edit ProviderConfigs
and reference elevated shared ClusterProviderConfigs as part of tenant admission.
This authentication source does not restrict other configurations a tenant is
otherwise allowed to use.

## Grant OCI access to the workload

For a disposable bucket test, a tenancy administrator can use this policy,
replacing the compartment and cluster placeholders. Repeat for tenant-b using
a different test compartment. Resource-specific operations may require additional
permissions; select the least privileges needed for the intended resource.

```text
Allow any-user to manage buckets in compartment <compartment-a> where all {
  request.principal.type = 'workload',
  request.principal.cluster_id = '<cluster-ocid>',
  request.principal.namespace = 'tenant-a',
  request.principal.service_account = 'oci-access'
}
```

The principal is native OKE `workload`, not the `identityfederateddomainapp`
principal used by generic non-OKE federation. See [Oracle's workload identity
documentation](https://docs.oracle.com/en-us/iaas/Content/ContEng/Tasks/contenggrantingworkloadaccesstoresources.htm).

## Renewal and lifecycle

Tokens remain in memory and are not written into Terraform configuration, Secrets
or ProviderConfig status. TokenRequests ask for one hour using the API-server's
default audience, matching the existing OKE CCM request shape. Actual expiry is
bounded by both the API response and JWT expiry. Concurrent refreshes on a cached
client are coalesced; refresh is lazy when OCI needs a token and starts between
70 and 80 percent of the granted lifetime if requested before expiry.

Refresh failures fail closed and back off from one to thirty seconds. An old JWT
is not returned after a refresh failure. Previously issued OCI credentials can
remain valid until the OCI SDK renews them. Removing RBAC does not immediately
revoke issued JWTs or OCI session credentials.

Account UID changes invalidate cached provider metadata on the next setup. A
supplier bound to an account that was recreated refuses new tokens until setup
resolves the new identity. In-flight OCI operations can retain their original
client; changing a ProviderConfig does not revoke credentials already issued.

Keep the ProviderConfig, ServiceAccount, RBAC and OCI policies available until
managed-resource deletion and finalizers complete. Before downgrading to a
provider without this source, migrate active configurations to a supported
authentication method and verify its access to existing OCI resources.

## Validation before production use

1. Confirm an opted-in provider runtime can read and request a token only for
   the named account in each tenant namespace; verify that another account and
   an ungranted namespace are denied on a real Kubernetes API server.
2. Reconcile two tenant resources with explicit namespaced ProviderConfig refs.
   Confirm their distinct workload identities in OCI Audit.
3. Verify each identity can operate in its own compartment and is denied access
   to the other tenant's compartment. Avoid broad pre-existing IAM grants that
   would mask a failure.
4. Continue across both Kubernetes token expiry and an actual OCI credential
   refresh without restarting the provider. Include a long-running OCI operation,
   provider restart, ServiceAccount recreation, and cache pressure.
5. Delete disposable managed resources before removing their permissions. Recheck
   existing Secret-based API keys, pod-based OKE identity and generic federation.

Unit and generated-schema tests do not prove OKE exchange, RBAC enforcement or
OCI IAM isolation. Capture actual cluster/SDK versions, granted lifetimes, request
IDs and sanitized Audit evidence for the release validation record.

## Troubleshooting

- `access denied`: check the RoleBinding targets the actual service provider
  ServiceAccount and grants named `serviceaccounts/token` creation in the tenant
  namespace, as well as ServiceAccount GET.
- `not found`: create the account in the ProviderConfig namespace.
- `missing a UID`, `replaced` or `being deleted`: wait for reconciliation to
  resolve a live account; do not bypass its UID checks.
- Invalid/short-lived token response: check TokenRequest expiration, server clock
  and the cluster's signing configuration. Do not paste JWTs into logs or tickets.
- OKE initialization/exchange failure: check Enhanced cluster support, runtime
  region/version settings, CA and network reachability. Diagnose using sanitized
  request IDs and OCI Audit; the provider deliberately suppresses raw token errors.

## Developing across both repositories

This development branch uses a `go.mod` replacement to pin the Terraform fork at
commit `fb8245ec3dd325f7beff63e549fbfd06504684fb`, containing the
`oci.InProcessOptions` extension. The pin is immutable; moving the Terraform
branch does not silently change the Crossplane dependency. A local Go workspace
is not required to build this branch.

Run the authentication and admission tests against the downloaded dependency:

```sh
GOWORK=off go test -race ./internal/clients ./apis/namespaced/v1beta1 -count=1
GOWORK=off go test ./cmd/generator ./config ./cmd/provider/config ./cmd/provider/compute ./cmd/provider/objectstorage -count=1
```

Build matching family/config and resource service packages before deploying a test
release. For the bucket scenario, include the objectstorage service provider.
Choose packages matching the OKE node architecture, install the new CRDs, and
retain the OKE runtime environment configuration when updating provider images.
Inspect OCI Audit and run the live validation checklist above before treating
the feature as production ready.

After the Terraform change is accepted upstream, remove the fork replacement,
update the original Terraform dependency to the upstream revision, and repeat
validation with `GOWORK=off`. Keep local workspace files out of releases. The old
upstream revision lacks these constructors and cannot compile this feature.
