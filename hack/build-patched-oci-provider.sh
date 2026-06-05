#!/usr/bin/env bash
set -euo pipefail

TF_PROVIDER_VERSION="${TF_PROVIDER_VERSION:-${TERRAFORM_PROVIDER_VERSION:-}}"
if [[ -z "${TF_PROVIDER_VERSION}" ]]; then
	echo "TF_PROVIDER_VERSION or TERRAFORM_PROVIDER_VERSION must be set" >&2
	exit 1
fi

GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"
WORKDIR="${WORKDIR:-_output/terraform-provider-src}"
SRC_DIR="${SRC_DIR:-${WORKDIR}/terraform-provider-oci}"
OUT_DIR="${OUT_DIR:-_output/terraform-provider/${GOOS}_${GOARCH}}"
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
case "${SRC_DIR}" in
	/*) SRC_DIR_ABS="${SRC_DIR}" ;;
	*) SRC_DIR_ABS="${ROOT_DIR}/${SRC_DIR}" ;;
esac
case "${OUT_DIR}" in
	/*) OUT_DIR_ABS="${OUT_DIR}" ;;
	*) OUT_DIR_ABS="${ROOT_DIR}/${OUT_DIR}" ;;
esac

if [[ ! -d "${SRC_DIR_ABS}" ]]; then
	echo "patched source directory ${SRC_DIR} does not exist; run hack/patch-tf-provider.sh first" >&2
	exit 1
fi

mkdir -p "${OUT_DIR_ABS}"

echo "==> Building patched terraform-provider-oci v${TF_PROVIDER_VERSION} for ${GOOS}/${GOARCH}"
(
	cd "${SRC_DIR_ABS}"
	GOOS="${GOOS}" GOARCH="${GOARCH}" go build \
		-o "${OUT_DIR_ABS}/terraform-provider-oci_v${TF_PROVIDER_VERSION}" \
		.
)

echo "==> Built ${OUT_DIR}/terraform-provider-oci_v${TF_PROVIDER_VERSION}"
