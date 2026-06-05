#!/usr/bin/env bash
set -euo pipefail

TF_PROVIDER_VERSION="${TF_PROVIDER_VERSION:-${TERRAFORM_PROVIDER_VERSION:-}}"
if [[ -z "${TF_PROVIDER_VERSION}" ]]; then
	echo "TF_PROVIDER_VERSION or TERRAFORM_PROVIDER_VERSION must be set" >&2
	exit 1
fi

CLONE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/terraform-provider-oci-validate.XXXXXX")"
REPO="${TERRAFORM_PROVIDER_REPO:-https://github.com/oracle/terraform-provider-oci}"
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PATCH_FILE="${PATCH_FILE:-hack/oci-provider-workload-identity.patch}"
case "${PATCH_FILE}" in
	/*) PATCH_FILE_ABS="${PATCH_FILE}" ;;
	*) PATCH_FILE_ABS="${ROOT_DIR}/${PATCH_FILE}" ;;
esac

cleanup() {
	rm -rf "${CLONE_DIR}"
}
trap cleanup EXIT

echo "==> Validating workload identity patch against v${TF_PROVIDER_VERSION}"
git clone -c advice.detachedHead=false --depth 1 --branch "v${TF_PROVIDER_VERSION}" \
	"${REPO}" "${CLONE_DIR}"

git -C "${CLONE_DIR}" apply --check "${PATCH_FILE_ABS}"

echo "==> Patch validates cleanly against v${TF_PROVIDER_VERSION}"
