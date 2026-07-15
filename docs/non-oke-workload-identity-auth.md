# Non-OKE Kubernetes Workload Identity Federation

Use this authentication method when an OCI Crossplane provider runs in a Kubernetes cluster and should authenticate to OCI with a projected Kubernetes service account token instead of an OCI API key.

This flow is intended for non-OKE Kubernetes clusters, including self-managed clusters. It uses an OCI Identity Propagation Trust to trust the provider pod's Kubernetes token, then uses OCI IAM policies to decide what that federated workload can manage.

For OKE clusters, prefer `auth = "OKEWorkloadIdentity"` unless you specifically need the generic token exchange flow described here.

## Setup Order

1. Mount a projected Kubernetes service account token into the provider pod.
2. Decode the provider pod token and record the token `iss` and `sub` claims.
3. Fetch the Kubernetes JWKS and convert the issuer RSA JWK to PEM.
4. Create or reuse an OCI identity domain.
5. Create an OCI Identity Propagation Trust for the Kubernetes issuer.
6. Add OCI IAM policies for token exchange and resource access.
7. Create the Crossplane credentials secret.
8. Create the ProviderConfig.
9. Apply a managed resource and verify reconciliation.

## Runtime Token Mount

The provider pod must mount the projected token at the path configured in the credentials secret. The examples below use:

```text
/var/run/secrets/tokens/oci
```

Create a shared service account first. Reusing this account across OCI provider packages gives all provider pods the same token subject, so one IAM policy can authorize them.

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: crossplane-provider-oci
  namespace: crossplane-system
```

Create a `DeploymentRuntimeConfig` that uses this service account and projects its token into each provider pod:

```yaml
apiVersion: pkg.crossplane.io/v1beta1
kind: DeploymentRuntimeConfig
metadata:
  name: oci-wif-runtime
spec:
  deploymentTemplate:
    spec:
      selector: {}
      template:
        spec:
          serviceAccountName: crossplane-provider-oci
          containers:
          - name: package-runtime
            volumeMounts:
            - name: oci-wif-token
              mountPath: /var/run/secrets/tokens
              readOnly: true
          volumes:
          - name: oci-wif-token
            projected:
              sources:
              - serviceAccountToken:
                  path: oci
                  expirationSeconds: 3600
                  audience: https://kubernetes.default.svc.cluster.local
```

Attach the runtime config to each OCI provider package that should use workload identity federation:

```yaml
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-oci-networking
spec:
  package: ghcr.io/oracle/provider-oci-networking:<version>
  runtimeConfigRef:
    name: oci-wif-runtime
```

## Collect Kubernetes Token Values

After the provider pod is running, find the provider pod:

```bash
kubectl -n crossplane-system get pods
```

If all OCI provider packages use the same `DeploymentRuntimeConfig` and service account, decode the token from one provider pod only. The `iss` and `sub` claims are shared. Check multiple pods only to verify the token mount, or when providers use different service accounts.

Decode the mounted token payload:

```bash
kubectl -n crossplane-system exec <provider-pod> -c package-runtime -- sh -lc '
TOKEN=$(cat /var/run/secrets/tokens/oci)
echo "$TOKEN" | cut -d . -f2 | base64 -d 2>/dev/null
'
```

Record these values from the decoded token:

```text
K8S_ISSUER=<iss>
K8S_SUBJECT=<sub>
```

The subject usually has this form:

```text
system:serviceaccount:crossplane-system:crossplane-provider-oci
```

Fetch the Kubernetes OIDC metadata and JWKS:

```bash
kubectl get --raw /.well-known/openid-configuration
kubectl get --raw /openid/v1/jwks > jwks.json
```

Convert the RSA JWK from `jwks.json` to PEM. That PEM is used as the Identity Propagation Trust `publicCertificate`.

The Identity Propagation Trust `publicCertificate` is for OCI IAM trust setup. It is not the same as `token_exchange_public_key`.

## Identity Domain and OAuth Application

Create or reuse an OCI identity domain. Record its domain URL and OCID; the URL is used as `token_exchange_domain_url`, and the OCID is used in the federated workload IAM policy.

For `token_exchange_auth = "OAuthClientCredentials"`, create an Integrated Application of type Confidential Application in the identity domain. Enable client configuration with these settings:

- Client credentials grant enabled.
- Client type `Confidential`.
- Authorized resources `All`.
- Application status `Active`.

Record the OAuth client ID and client secret. When the Identity Propagation Trust is created through the identity domain Admin REST API, assign the application the `Identity Domain Administrator` app role so its client-credentials access token can call the Admin API. This administrator client is also needed as a bootstrap credential when provisioning a trust for the `InstancePrincipal` runtime path; it is separate from the runtime token-exchange authorization.

## Create Identity Propagation Trust

Create the trust in the identity domain that will perform token exchange. The trust must use the Kubernetes issuer and the PEM public certificate from the previous step.

Use these values:

```text
issuer=<K8S_ISSUER>
publicCertificate=<Kubernetes issuer public key PEM>
type=JWT
subjectType=Resource
active=true
allowImpersonation=true
impersonatingResource=k8sworkload
claimPropagations=["ext_iss"]
```

The `impersonatingResource` value must match the Crossplane credential value:

```json
"token_exchange_resource_type": "k8sworkload"
```

If using OAuth client credentials for token exchange, allow the OAuth confidential application client in the Identity Propagation Trust.

The following REST flow creates the trust for OAuth client credentials. It requires `jq`, `curl`, and a PEM file produced from the Kubernetes JWKS.

```bash
DOMAIN_URL="https://<identity-domain-url>"
CLIENT_ID="<oauth-client-id>"
CLIENT_SECRET="<oauth-client-secret>"
TRUST_NAME="crossplane-kubernetes-wif"
K8S_ISSUER="<issuer-from-provider-token>"
PUBLIC_CERT_FILE="/path/to/kubernetes-issuer-public-key.pem"

curl -sS -u "$CLIENT_ID:$CLIENT_SECRET" \
  -H "Content-Type: application/x-www-form-urlencoded;charset=UTF-8" \
  -X POST "$DOMAIN_URL/oauth2/v1/token" \
  -d "grant_type=client_credentials&scope=urn:opc:idm:__myscopes__" \
  | jq -r '.access_token' > /private/tmp/domain-token.tok

jq -n \
  --arg name "$TRUST_NAME" \
  --arg issuer "$K8S_ISSUER" \
  --rawfile publicCertificate "$PUBLIC_CERT_FILE" \
  --arg oauthClient "$CLIENT_ID" \
  '{
    name: $name,
    issuer: $issuer,
    publicCertificate: $publicCertificate,
    type: "JWT",
    subjectType: "Resource",
    active: true,
    allowImpersonation: true,
    impersonatingResource: "k8sworkload",
    schemas: ["urn:ietf:params:scim:schemas:oracle:idcs:IdentityPropagationTrust"],
    oauthClients: [$oauthClient],
    claimPropagations: ["ext_iss"]
  }' > /private/tmp/identity-propagation-trust.json

curl -sS -X POST "$DOMAIN_URL/admin/v1/IdentityPropagationTrusts" \
  -H "Authorization: Bearer $(cat /private/tmp/domain-token.tok)" \
  -H "Content-Type: application/scim+json" \
  -H "Accept: application/scim+json" \
  --data @/private/tmp/identity-propagation-trust.json
```

For `InstancePrincipal`, use an identity domain administrator credential to create the trust, but omit `oauthClients` from the trust payload. The `impersonatingResource` value is independent of the token exchange authorization mode and must still match `token_exchange_resource_type`.

## IAM Policies

The federated workload needs an OCI IAM policy for the resources it will manage after token exchange succeeds.

For VCN testing:

```text
Allow any-user to manage virtual-network-family in compartment <compartment-name> where all {
  request.principal.type = 'identityfederateddomainapp',
  request.principal.name = '<K8S_SUBJECT>',
  request.principal.domain.id = '<IDENTITY_DOMAIN_OCID>',
  request.principal.ext_iss = '<K8S_ISSUER>'
}
```

For Object Storage testing:

```text
Allow any-user to manage object-family in compartment <compartment-name> where all {
  request.principal.type = 'identityfederateddomainapp',
  request.principal.name = '<K8S_SUBJECT>',
  request.principal.domain.id = '<IDENTITY_DOMAIN_OCID>',
  request.principal.ext_iss = '<K8S_ISSUER>'
}
```

When using `token_exchange_auth = "InstancePrincipal"`, the provider pod must run on OCI Compute worker nodes. Create a dynamic group that matches the worker instances, for example:

```text
ALL {instance.compartment.id = '<worker-node-compartment-ocid>'}
```

Then allow those workers to request RPST tokens:

```text
Allow dynamic-group <worker-node-dynamic-group> to {GET_RPST} in tenancy
```

You still need the Identity Propagation Trust and the federated workload resource policy. The dynamic group policy only authorizes the OCI worker instance principal to perform token exchange.

`OAuthClientCredentials` does not need the dynamic group or `{GET_RPST}` policy. It uses the confidential application credentials to authorize the token exchange request.

## ProviderConfig Credentials

For OAuth client credential token exchange:

```bash
kubectl create secret generic oci-creds \
  --namespace=crossplane-system \
  --from-literal=credentials='{
  "auth": "WorkloadIdentityFederation",
  "region": "us-ashburn-1",
  "workload_identity_token_path": "/var/run/secrets/tokens/oci",
  "token_exchange_domain_url": "https://<identity-domain-url>",
  "token_exchange_auth": "OAuthClientCredentials",
  "token_exchange_client_id": "<oauth-client-id>",
  "token_exchange_client_secret": "<oauth-client-secret>",
  "token_exchange_requested_token_type": "urn:oci:token-type:oci-rpst",
  "token_exchange_subject_token_type": "jwt",
  "token_exchange_resource_type": "k8sworkload",
  "token_exchange_rpst_exp": "3600"
  }'
```

For Instance Principal token exchange:

```bash
kubectl create secret generic oci-creds \
  --namespace=crossplane-system \
  --from-literal=credentials='{
  "auth": "WorkloadIdentityFederation",
  "region": "us-ashburn-1",
  "workload_identity_token_path": "/var/run/secrets/tokens/oci",
  "token_exchange_domain_url": "https://<identity-domain-url>",
  "token_exchange_auth": "InstancePrincipal",
  "token_exchange_requested_token_type": "urn:oci:token-type:oci-rpst",
  "token_exchange_subject_token_type": "jwt",
  "token_exchange_resource_type": "k8sworkload",
  "token_exchange_rpst_exp": "3600"
  }'
```

Instance Principal mode omits `token_exchange_client_id` and `token_exchange_client_secret`.

## Credential Field Behavior

The Terraform provider version 8.22.0 validates these credential fields when `auth` is `WorkloadIdentityFederation`:

| Field | Behavior |
| --- | --- |
| `workload_identity_token_path`, `token_exchange_domain_url`, `region`, `token_exchange_requested_token_type` | Required. |
| `token_exchange_auth` | Optional; defaults to `OAuthClientCredentials`. |
| `token_exchange_client_id`, `token_exchange_client_secret` | Required for `OAuthClientCredentials`; must not be set for `InstancePrincipal`. |
| `token_exchange_subject_token_type` | Optional in the provider schema. Set it explicitly to `jwt` for Kubernetes service account tokens. |
| `token_exchange_resource_type` | Required when `token_exchange_requested_token_type` is `urn:oci:token-type:oci-rpst`; it must match the trust `impersonatingResource`. |
| `token_exchange_rpst_exp` | Optional. |
| `token_exchange_public_key` | Optional; it is not the PEM certificate used by the Identity Propagation Trust. |

## ProviderConfig

For modern namespaced resources, create a `ClusterProviderConfig`:

```yaml
apiVersion: oci.m.upbound.io/v1beta1
kind: ClusterProviderConfig
metadata:
  name: default
spec:
  credentials:
    source: Secret
    secretRef:
      namespace: crossplane-system
      name: oci-creds
      key: credentials
```

For legacy cluster-scoped resources, create a `ProviderConfig`:

```yaml
apiVersion: oci.upbound.io/v1beta1
kind: ProviderConfig
metadata:
  name: default
spec:
  credentials:
    source: Secret
    secretRef:
      namespace: crossplane-system
      name: oci-creds
      key: credentials
```

## Validation

Apply a small managed resource, such as a VCN or Object Storage bucket, and check the resource condition:

```bash
kubectl get managed
kubectl describe <resource-kind> <resource-name>
kubectl -n crossplane-system logs <provider-pod> -c package-runtime
```

If token exchange fails, verify these values first:

- The provider pod has `/var/run/secrets/tokens/oci`.
- `workload_identity_token_path` matches the mounted token path.
- `token_exchange_domain_url` matches the identity domain URL.
- `token_exchange_resource_type` matches the trust `impersonatingResource`.
- The IAM policy `request.principal.name` exactly matches the token `sub`.
- The IAM policy `request.principal.ext_iss` exactly matches the token `iss`.
- The trust `publicCertificate` was generated from the Kubernetes issuer JWKS.
