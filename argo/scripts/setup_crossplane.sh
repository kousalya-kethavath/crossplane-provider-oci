#!/bin/bash
set -euo pipefail

# Environment variables for customization
: "${NAMESPACE:=crossplane-system}"
: "${PROVIDERS:=provider-family-oci}"  # Comma-separated list of providers
: "${REGION:=us-ashburn-1}"
: "${KUBECONFIG:=}"  # Path to kubeconfig for target cluster
: "${CONTEXT:=}"  # Context name for target cluster
: "${PROVIDER_IMAGE_REPO_NAME:=ghcr.io/oracle}"  # OCI provider image repository
: "${FAMILY_PROVIDER_VERSION:=v1.0.1}"  # Version of OCI provider family
: "${SUB_PROVIDERS_VERSION:=${FAMILY_PROVIDER_VERSION}}"  # Version of sub-providers (defaults to FAMILY_PROVIDER_VERSION)
: "${TENANCY_OCID:=ocid1.tenancy.oc1.xxx}"
: "${TEST_NAMESPACES:=}"  # Comma-separated list of namespaces that should receive namespaced ProviderConfigs
: "${REPO_ROOT:=}"  # Repository root; defaults to /git-repo in Argo or this script's checkout root

if [[ -z "${REPO_ROOT}" ]]; then
  if [[ -d /git-repo/argo/setup ]]; then
    REPO_ROOT="/git-repo"
  else
    script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    REPO_ROOT="$(cd "${script_dir}/../.." && pwd)"
  fi
fi

# kubectl wrapper for multi-cluster support
kctl() {
  local cmd="kubectl"
  if [[ -n "${CONTEXT}" ]]; then
    cmd="${cmd} --context=${CONTEXT}"
  fi
  if [[ -n "${KUBECONFIG}" && -f "${KUBECONFIG}" ]]; then
    cmd="${cmd} --kubeconfig=${KUBECONFIG}"
  fi
  ${cmd} "$@"
}

hctl() {
  local cmd="helm"
  if [[ -n "${CONTEXT}" ]]; then
    cmd="${cmd} --kube-context=${CONTEXT}"
  fi
  if [[ -n "${KUBECONFIG}" && -f "${KUBECONFIG}" ]]; then
    cmd="${cmd} --kubeconfig=${KUBECONFIG}"
  fi
  ${cmd} "$@"
}

# Step 1: Create namespace and install Crossplane
echo "Creating namespace ${NAMESPACE}..."
kctl get ns "${NAMESPACE}" || kctl create ns "${NAMESPACE}"

echo "Installing Crossplane..."
hctl repo add crossplane-stable https://charts.crossplane.io/stable && hctl repo update
hctl install crossplane --namespace "${NAMESPACE}" crossplane-stable/crossplane
kctl wait --for=condition=available --timeout=300s deployment/crossplane -n "${NAMESPACE}"

# Step 2: Deploy OCI provider family and sub-providers
echo "Deploying OCI providers..."
for provider in ${PROVIDERS//,/ }; do
  if [[ "${provider}" == "provider-family-oci" ]]; then
    # Derive provider name from PROVIDER_IMAGE_REPO_NAME. E.g., for ghcr.io/oracle, it becomes oracle-provider-family-oci, for iad.ocir.io/<compartment>/<user>, it becomes <compartment>-<user>-provider-family-oci
    family_provider_prefix=$(echo "${PROVIDER_IMAGE_REPO_NAME}" | cut -d/ -f2- | tr '/' '-')
    provider_name="${family_provider_prefix}-${provider}"
    echo "Using family provider name: ${provider_name}"
    VERSION="${FAMILY_PROVIDER_VERSION}"
  else
    provider_name="${provider}"
    echo "Using sub-provider name: ${provider_name}"
    VERSION="${SUB_PROVIDERS_VERSION}"
  fi
  cat <<EOF | kctl apply -f -
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: ${provider_name}
spec:
  package: ${PROVIDER_IMAGE_REPO_NAME}/${provider}:${VERSION}
EOF
done

# Verify providers are installed and healthy
echo "Verifying OCI providers..."
kctl wait --for=condition=healthy --timeout=300s providers --all
timeout=300  # 5 minutes
interval=30  # 30 seconds
end_time=$(( $(date +%s) + timeout ))
while true; do
   if kctl get provider -o json | jq -e '
    .items[]
    | {
        healthy: (.status.conditions[]? | select(.type=="Healthy") | .status // ""),
        installed: (.status.conditions[]? | select(.type=="Installed") | .status // "")
        }
    | select(.healthy != "True" or .installed != "True")
    ' | grep .;then
        if [[ $(date +%s) -ge ${end_time} ]]; then
        echo "Timeout reached. Some providers are not healthy or installed."
        kctl get providers
        exit 1
        fi
        echo "Some providers are not yet healthy or installed. Retrying in ${interval} seconds..."
        sleep ${interval}
    else
        echo "All OCI providers are installed and healthy."
        break
    fi
done


# Step 3: Create InstancePrincipal secrets (cluster namespace + test namespaces)
credentials_payload="{
    \"tenancy_ocid\": \"${TENANCY_OCID}\",
    \"auth\": \"InstancePrincipal\",
    \"region\": \"${REGION}\"
  }"

echo "Creating InstancePrincipal secret in ${NAMESPACE}..."
kctl create secret generic oci-creds \
  --namespace="${NAMESPACE}" \
  --from-literal=credentials="${credentials_payload}" \
  --dry-run=client -o yaml | kctl apply -f -

target_namespaces=()
IFS=',' read -r -a raw_namespaces <<< "${TEST_NAMESPACES}"
for target_namespace in "${raw_namespaces[@]}"; do
  target_namespace="$(echo "${target_namespace}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
  if [[ -n "${target_namespace}" ]]; then
    target_namespaces+=("${target_namespace}")
  fi
done

for target_namespace in "${target_namespaces[@]}"; do
  echo "Ensuring namespace ${target_namespace} exists..."
  kctl get ns "${target_namespace}" || kctl create ns "${target_namespace}"

  echo "Creating InstancePrincipal secret in ${target_namespace}..."
  kctl create secret generic oci-creds \
    --namespace="${target_namespace}" \
    --from-literal=credentials="${credentials_payload}" \
    --dry-run=client -o yaml | kctl apply -f -
done

# Step 4: Create ProviderConfigs
echo "Applying legacy cluster ProviderConfig..."
CROSSPLANE_NAMESPACE="${NAMESPACE}" \
  envsubst < "${REPO_ROOT}/argo/setup/providerconfig.yaml" | kctl apply -f -

echo "Applying managed cluster ProviderConfig..."
CROSSPLANE_NAMESPACE="${NAMESPACE}" \
  envsubst < "${REPO_ROOT}/argo/setup/cluster-providerconfig.yaml" | kctl apply -f -

for target_namespace in "${target_namespaces[@]}"; do
  echo "Applying namespaced ProviderConfig in ${target_namespace}..."
  TARGET_NAMESPACE="${target_namespace}" \
    envsubst < "${REPO_ROOT}/argo/setup/namespaced-providerconfig.yaml" | kctl apply -f -
done

echo "Crossplane setup with OCI providers completed successfully."
