# Quick Start

This guide walks through the process to install the OCI official provider-family.

To use OCI official provider-family, make sure you have installed Crossplane into your Kubernetes cluster. Follow installation of the `Provider`, applying a `ProviderConfig`, and creating the managed resource in OCI via Kubernetes.

## Install the official OCI provider-family
Install the official provider-family into the Kubernetes cluster with a Kubernetes configuration file. For instance, let's install the `provider-oci-objectstorage`

The first provider installed of a family also installs an extra provider-family Provider. The provider-family provider manages the `ProviderConfig` for all other providers in the same family.

> [!NOTE]
> Always install the family provider first to ensure anticipated/right version of image is pulled. Installing sub-provider first creates a missing dependency issue, which the crossplane package-manager always resolves by pulling latest family provider image. It might lead to unexpected behavior. 
 
> [!IMPORTANT]
> Adhere to the following naming format for family provider as: `(organization)-(provider-name)`, eg: oracle-provider-family-oci. 
> When pulling the image from a registry, crossplane refers to the name as `registry.io/organization/provider-name:tags`, eg: ghcr.io/oracle/provider-family-oci:v0.0.2.
> Crossplane behavior and corresponding steps to resolve conflict detailed in section: [Owner references conflict](#owner-references-conflict)

```bash
cat <<EOF | kubectl apply -f -
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: oracle-provider-family-oci
spec:
  package: ghcr.io/oracle/provider-family-oci:v0.0.2
---
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-oci-objectstorage
spec:
  package: ghcr.io/oracle/provider-oci-objectstorage:v0.0.2
EOF
```

Apply this configuration with `kubectl apply -f`.

After installing the provider, verify the install with `kubectl get providers`.

```bash
kubectl get providers
```         
```        
# Sample output
NAME                                INSTALLED   HEALTHY   PACKAGE                                                          AGE
oracle-provider-oci-family          True        True      ghcr.io/oracle/provider-family-oci:v0.0.2          3m3s
provider-oci-objectstorage          True        True      ghcr.io/oracle/provider-oci-objectstorage:v0.0.2   3m2s
```

It may take up to 5 minutes to report `HEALTHY`.

### Optional: Pulling images from private registry
If you are required to pull images from private registries instead of `ghcr.io`. Mirror the images into, for example `registry1.io`, then utilize the following `ImageConfig` sample to rewrite image paths and configure a pull secret. 

[The official crossplane documents](https://docs.crossplane.io/latest/packages/image-configs/#rewriting-image-paths) covers the rewrite image paths in more details.
```
---
apiVersion: pkg.crossplane.io/v1beta1
kind: ImageConfig
metadata:
  name: private-registry-rewrite
spec:
  matchImages:
    - prefix: ghcr.io
  rewriteImage:
    prefix: registry1.io

# Configure pull secrets for registry1.io
---
apiVersion: pkg.crossplane.io/v1beta1
kind: ImageConfig
metadata:
  name: private-registry-auth
spec:
  matchImages:
    - type: Prefix
      prefix: registry1.io
  registry:
    authentication:
      pullSecretRef:
        name: private-registry-credentials
```

OCI service sub-packages such as `provider-oci-compute` depend on `provider-family-oci` for authentication. If you rewrite only the service package image, Crossplane still resolves the family dependency from `ghcr.io`. Mirror both packages under a shared prefix and rewrite that shared prefix instead.

For example, if you mirror OCI packages to `iad.ocir.io/<tenancy-namespace>/<repository-prefix>/<package-name>` and preserve the upstream package names and tags, use:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: pkg.crossplane.io/v1beta1
kind: ImageConfig
metadata:
  name: rewrite-oci-packages-to-ocir
spec:
  matchImages:
    - prefix: ghcr.io/oracle
  rewriteImage:
    prefix: iad.ocir.io/<tenancy-namespace>/<repository-prefix>
---
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: oracle-provider-oci-compute
spec:
  package: ghcr.io/oracle/provider-oci-compute:<version>
EOF
```

If you need to rewrite a specific OCI package image path or tag as well, add a second `ImageConfig` with a longer matching prefix for that exact package reference. Crossplane selects the longest matching prefix, so the specific rule overrides the broad OCI rule for that package while the broad rule still rewrites dependencies such as `provider-family-oci`.

```bash
cat <<EOF | kubectl apply -f -
apiVersion: pkg.crossplane.io/v1beta1
kind: ImageConfig
metadata:
  name: rewrite-oci-packages-to-ocir
spec:
  matchImages:
    - prefix: ghcr.io/oracle
  rewriteImage:
    prefix: iad.ocir.io/<tenancy-namespace>/<repository-prefix>
---
apiVersion: pkg.crossplane.io/v1beta1
kind: ImageConfig
metadata:
  name: rewrite-oci-compute-image-and-tag
spec:
  matchImages:
    - prefix: ghcr.io/oracle/provider-oci-compute:v0.2.0-test
  rewriteImage:
    prefix: iad.ocir.io/<tenancy-namespace>/<repository-prefix>/provider-oci-compute:v0.2.0-ocir
---
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: oracle-provider-oci-compute
spec:
  package: ghcr.io/oracle/provider-oci-compute:v0.2.0-test
EOF
```

If the mirrored family package also uses a different tag, add another specific `ImageConfig` for `ghcr.io/oracle/provider-family-oci:<version>`.

If you mirror only selected OCI packages, ensure `provider-family-oci` is mirrored alongside each OCI service package or install the family provider separately from a reachable registry before installing the service sub-package.

## Configure family provider for OCI

The official provider-family requires credentials to create and manage OCI resources. Choose one of the following authentication methods:

### API Key Authentication

This is the traditional method using OCI API keys.

1. Create a secret by using the following command.
   ```bash
   kubectl create secret generic oci-creds \
   --namespace=crossplane-system \
   --from-literal=credentials='{
   "tenancy_ocid": "REPLACE_WITH_YOUR_TENANCY_OCID",
   "user_ocid": "REPLACE_WITH_YOUR_USER_OCID",
   "private_key": "-----BEGIN RSA PRIVATE KEY-----\nMIIE...REPLACE_WITH_YOUR_KEY_CONTENT...AB\n-----END RSA PRIVATE KEY-----\n",
   "fingerprint": "REPLACE_WITH_YOUR_FINGERPRINT",
   "region": "REPLACE_WITH_YOUR_REGION",
   "auth": "ApiKey"
   }'
   ```
   `private_key` must be the full PEM private key with newline characters escaped as `\n` and a trailing newline at the end.
   If your key content includes additional lines (for example, `OCI_API_KEY`), keep them inside the same quoted `private_key` value and escape each newline as `\n`.
   **Note:** Refer to `examples/providerconfig/secret.yaml.tmpl` for all available options. Additional reference [SDKConfig](https://docs.oracle.com/en-us/iaas/Content/API/Concepts/sdkconfig.htm).

### Instance Principal Authentication

This method uses OCI Instance Principal for authentication when running on OCI compute instances.

1. Create a secret by using the following command.
   ```bash
   kubectl create secret generic oci-creds \
   --namespace=crossplane-system \
   --from-literal=credentials='{
   "tenancy_ocid": "REPLACE_WITH_YOUR_TENANCY_OCID",
   "auth": "InstancePrincipal",
   "region": "REPLACE_WITH_YOUR_REGION"
   }'
   ```

### Workload Identity Authentication

This method uses OCI Workload Identity for authentication in OKE environments.

1. Create a service account for the provider.
   ```bash
   cat <<EOF | kubectl apply -f -
   apiVersion: v1
   kind: ServiceAccount
   metadata:
     name: crossplane-provider-oci
     namespace: crossplane-system
   EOF
   ```

2. Create the necessary IAM policies in OCI.

   Create policies that allow the workload identity to access OCI resources. Example policy statement (replace placeholders with your values):

   ```
   Allow any-user to <verb> <resource> in <location> where all {
   request.principal.type = 'workload',
   request.principal.namespace = '<namespace-name>',
   request.principal.service_account = '<service-account-name>',
   request.principal.cluster_id = '<cluster-ocid>'}
   ```

3. Configure provider runtime and credentials for Workload Identity.

   ```bash
   cat <<EOF | kubectl apply -f -
   ---
   apiVersion: pkg.crossplane.io/v1beta1
   kind: DeploymentRuntimeConfig
   metadata:
     name: oci-environment-config
   spec:
     deploymentTemplate:
       spec:
         selector: {}
         template:
           spec:
             containers:
               - name: package-runtime
                 env:
                   - name: OCI_RESOURCE_PRINCIPAL_VERSION # Set the required environment variables for Workload Identity
                     value: "2.2"
                   - name: OCI_RESOURCE_PRINCIPAL_REGION
                     value: REPLACE_WITH_YOUR_REGION
                   - name: OCI_RESOURCE_PRINCIPAL_WORKLOAD_IDENTITY
                     value: "1"
   ---
   apiVersion: pkg.crossplane.io/v1
   kind: Provider
   metadata:
     name: oracle-provider-family-oci
   spec:
     package: ghcr.io/oracle/provider-family-oci:v0.0.2
     runtimeConfigRef:
       name: oci-environment-config
   ---
   apiVersion: pkg.crossplane.io/v1
   kind: Provider
   metadata:
     name: provider-oci-objectstorage
   spec:
     package: ghcr.io/oracle/provider-oci-objectstorage:v0.0.2
     runtimeConfigRef:
       name: oci-environment-config
   ---
   apiVersion: v1
   kind: Secret
   metadata:
     name: oci-creds
     namespace: crossplane-system
   type: Opaque
   stringData:
     credentials: |
       {
         "auth": "OKEWorkloadIdentity",
         "region": "REPLACE_WITH_YOUR_REGION"
       }
   EOF
   ```

After choosing one authentication method above, register the provider configuration:
```bash
cat <<EOF | kubectl apply -f -
apiVersion: oci.m.upbound.io/v1beta1
kind: ClusterProviderConfig
metadata:
  name: default
spec:
  credentials:
    source: Secret
    secretRef:
      name: oci-creds
      namespace: crossplane-system
      key: credentials
---
apiVersion: oci.upbound.io/v1beta1
kind: ProviderConfig
metadata:
  name: default
spec:
  credentials:
    source: Secret
    secretRef:
      name: oci-creds
      namespace: crossplane-system
      key: credentials
EOF
```

> [!NOTE]
> If the provider configuration is incorrect (for example, wrong credentials or authentication settings), managed resources can report `READY=False` and `SYNCED=False` until the configuration is fixed.
>
> `ClusterProviderConfig.oci.m.upbound.io` is the modern default for namespaced managed resources (`*.oci.m.upbound.io`).
> Legacy cluster-scoped managed resources (`*.oci.upbound.io`) continue to use `ProviderConfig.oci.upbound.io`.


## Create a managed resource

Create a managed resource to verify the provider-oci-objectstorage is functioning.

Use this command to instruct Crossplane to create the bucket in the OCI tenancy.

```bash
# Edit examples/objectstorage/v1alpha1/bucket.yaml with your compartment and storage namespace as documented.
# Apply the example that creates an Object Storage bucket

kubectl apply -f examples/objectstorage/v1alpha1/bucket.yaml
```

For namespaced managed resources (`apiVersion: *.oci.m.upbound.io/*`), set:
```yaml
metadata:
  namespace: upbound-system
spec:
  providerConfigRef:
    kind: ClusterProviderConfig
    name: default
```

For local namespaced secret references inside namespaced resources, do not set `namespace` in the local ref fields.

Verify the status of the resource by running this command (example output is shown).
```bash
kubectl get managed
```
```
# Sample output
NAME                                                         READY   SYNCED   EXTERNAL-NAME                            AGE
bucket.objectstorage.oci.upbound.io/bucket-via-crossplane4   True    True     n/idimd1fghobk/b/bucket-via-crossplane   10m
```
Upbound created the bucket when the values `READY` and `SYNCED` are True. This may take up to 5 minutes.

If the `READY` or `SYNCED` are blank or False use kubectl describe to understand why.

Here is an example of a failure because the `spec.providerConfigRef.name` value in the `Bucket` doesn't match the `ProviderConfig` `metadata.name`.

```bash
kubectl describe bucket
```
```
# Sample output
Name:         bucket-via-crossplane4
Namespace:    
Labels:       provider=oci
Annotations:  <none>
API Version:  objectstorage.oci.upbound.io/v1alpha1
Kind:         Bucket
# Output truncated
Spec:
  Deletion Policy:  Delete
  For Provider:
    Compartment Id:  ocid1.compartment.oc1..xxx
    Name:            bucket-via-crossplane
    Namespace:       id8pypxcqtqs
  Management Policies:
    *
  Provider Config Ref:
    Name:  default
Status:
  At Provider:
  Conditions:
    Last Transition Time:  2025-09-05T20:07:20Z
    Message:               connect failed: cannot get terraform setup: cannot get referenced ProviderConfig: ProviderConfig.oci.upbound.io "default" not found
    Observed Generation:   1
    Reason:                ReconcileError
    Status:                False
    Type:                  Synced
Events:
  Type     Reason                   Age               From                                                        Message
  ----     ------                   ----              ----                                                        -------
  Warning  CannotConnectToProvider  3s (x4 over 10s)  managed/objectstorage.oci.upbound.io/v1alpha1, kind=bucket  cannot get terraform setup: cannot get referenced ProviderConfig: ProviderConfig.oci.upbound.io "default" not found
```

The output indicates the `Bucket` is using a `ProviderConfig` named `default`. The applied `ProviderConfig` is `oci-provider`.

```bash
kubectl get clusterproviderconfigs.oci.m.upbound.io
kubectl get providerconfigs.oci.upbound.io
```

## Delete the managed resource

Remove the managed resource by using `kubectl delete -f examples/objectstorage/v1alpha1/bucket.yaml`. Verify removal of the bucket with kubectl get buckets

```bash
kubectl delete -f examples/objectstorage/v1alpha1/bucket.yaml
```
```
# Sample output
bucket.objectstorage.oci.upbound.io "bucket-via-crossplane4" deleted
```

```bash
kubectl get buckets
```
```
# Sample output
No resources found
```
> [!Warning]
> Never delete providers before deleting managed resources to avoid dangling resource.

## Delete the providers

Remove the installed providers by
```bash
kubectl delete providers/provider-oci-objectstorage

kubectl delete providerconfig/default

kubectl delete providers/oracle-provider-oci-family
```

> [!NOTE]
> The package manager requires that the `oracle-provider-oci-family` image be pulled from the same registry if any sub-provider is installed through pull. Ensure consistency in the image source to avoid conflicts.

> [!Warning]
> Never delete a family provider before deleting its sub-providers. Deleting a family provider while sub-providers are still installed can lead to unexpected behavior and potential errors.

## Owner references conflict
Crossplane’s package manager establishes an ownership chain between a family Provider (parent) and each sub‑provider (child) using metadata.ownerReferences on the child’s ProviderRevision. The owner reference encodes the parent Provider’s “package identity” as a name that the package manager expects to persist:

```bash
kubectl describe providerrevisions/oracle-provider-family-oci...
```
```
# Sample output
  <TRUNCATED OUTPUT>
  Owner References:
    API Version:           pkg.crossplane.io/v1
    Block Owner Deletion:  true
    Controller:            true
    Kind:                  Provider
    Name:                  oracle-provider-family-oci
    UID:                   ...
```

What can go wrong
- If the family Provider is deleted or reinstalled with a different metadata.name while any sub‑providers or their ProviderRevisions still exist, Crossplane will attempt to satisfy the outstanding owner reference by automatically pulling a Provider with the name encoded in the ownerReferences (from the same registry as the sub‑provider’s image).
- This results in two family Providers trying to coexist (the one you applied and the one auto‑installed to satisfy the ownerRef), which leads to unhealthy/competing ProviderRevisions and confusing state during upgrades.

Typical symptoms
- An unexpected Provider with name - appears after you delete or rename your family Provider.
- providerrevisions stuck in Unhealthy/Inactive with messages about conflicting ownership or multiple controlling owners.
- Repeated reconcile loops pulling a family image you did not specify explicitly.

### Resolving duplicate family providers 

Proceed with clean up of managed resource(s) and providers in order. Refer to the sections: [Delete the managed resource](#delete-the-managed-resource) and [Delete the providers](#delete-the-providers).

After deletion, check for the existence of duplicate family provider by 
```bash
kubectl get providers
```

If exists, delete the duplicated family provider by
```bash
kubectl delete providers/<duplicate-provider-name>
```
