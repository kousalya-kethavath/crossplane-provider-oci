#!/usr/bin/env bash
set -euo pipefail

TF_PROVIDER_VERSION="${TF_PROVIDER_VERSION:-${TERRAFORM_PROVIDER_VERSION:-}}"
if [[ -z "${TF_PROVIDER_VERSION}" ]]; then
	echo "TF_PROVIDER_VERSION or TERRAFORM_PROVIDER_VERSION must be set" >&2
	exit 1
fi

WORKDIR="${WORKDIR:-_output/terraform-provider-src}"
PATCH_FILE="${PATCH_FILE:-hack/oci-provider-workload-identity.patch}"
REPO="${TERRAFORM_PROVIDER_REPO:-https://github.com/oracle/terraform-provider-oci}"
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
case "${PATCH_FILE}" in
	/*) PATCH_FILE_ABS="${PATCH_FILE}" ;;
	*) PATCH_FILE_ABS="${ROOT_DIR}/${PATCH_FILE}" ;;
esac
case "${WORKDIR}" in
	/*) WORKDIR_ABS="${WORKDIR}" ;;
	*) WORKDIR_ABS="${ROOT_DIR}/${WORKDIR}" ;;
esac
SRC_DIR="${WORKDIR_ABS}/terraform-provider-oci"

rm -rf "${SRC_DIR}"
mkdir -p "${WORKDIR_ABS}"

echo "==> Cloning terraform-provider-oci v${TF_PROVIDER_VERSION}"
git clone -c advice.detachedHead=false --depth 1 --branch "v${TF_PROVIDER_VERSION}" \
	"${REPO}" "${SRC_DIR}"

echo "==> Applying workload identity patch"
git -C "${SRC_DIR}" apply "${PATCH_FILE_ABS}"

echo "==> Patched source is ready at ${SRC_DIR}"
