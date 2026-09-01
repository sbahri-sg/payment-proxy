#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
output_dir="${1:-${repo_dir}/dist/provider-apps}"
bundle_tmp="$(mktemp -d "${TMPDIR:-/tmp}/emisell-ipaymu-provider-app.XXXXXX")"
trap 'rm -rf "$bundle_tmp"' EXIT INT TERM

mkdir -p "${output_dir}" "${bundle_tmp}/bundle/contract-tests" "${bundle_tmp}/bundle/src/ipaymu"
install -m 0644 "${repo_dir}/provider-apps/ipaymu/emisell-extension.yaml" "${bundle_tmp}/bundle/emisell-extension.yaml"
install -m 0644 "${repo_dir}/provider-apps/ipaymu/openapi.yaml" "${bundle_tmp}/bundle/openapi.yaml"
install -m 0644 "${repo_dir}/provider-apps/ipaymu/README.md" "${bundle_tmp}/bundle/README.md"
install -m 0644 "${repo_dir}/provider-apps/ipaymu/SECURITY.md" "${bundle_tmp}/bundle/SECURITY.md"
install -m 0644 "${repo_dir}/provider-apps/ipaymu/contract-tests/README.md" "${bundle_tmp}/bundle/contract-tests/README.md"
install -m 0644 "${repo_dir}/backend/internal/ipaymu/client.go" "${bundle_tmp}/bundle/src/ipaymu/client.go"
install -m 0644 "${repo_dir}/backend/internal/ipaymu/manifest.go" "${bundle_tmp}/bundle/src/ipaymu/manifest.go"

(
  cd "${bundle_tmp}/bundle"
  touch -t 198001010000 emisell-extension.yaml openapi.yaml README.md SECURITY.md contract-tests/README.md src/ipaymu/client.go src/ipaymu/manifest.go
  COPYFILE_DISABLE=1 zip -X -q "${bundle_tmp}/ipaymu-provider-app-emisell-v2.0.2.zip" \
    emisell-extension.yaml openapi.yaml README.md SECURITY.md contract-tests/README.md \
    src/ipaymu/client.go src/ipaymu/manifest.go
)

install -m 0644 "${bundle_tmp}/ipaymu-provider-app-emisell-v2.0.2.zip" "${output_dir}/ipaymu-provider-app-emisell-v2.0.2.zip"
shasum -a 256 "${output_dir}/ipaymu-provider-app-emisell-v2.0.2.zip"
