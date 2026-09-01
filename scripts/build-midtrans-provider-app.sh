#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_dir="${repo_dir}/dist/provider-apps"
bundle_tmp="$(mktemp -d "${TMPDIR:-/tmp}/emisell-midtrans-provider-app.XXXXXX")"
trap 'rm -rf "${bundle_tmp}"' EXIT

mkdir -p "${output_dir}" "${bundle_tmp}/bundle/contract-tests" "${bundle_tmp}/bundle/src/midtrans"
install -m 0644 "${repo_dir}/provider-apps/midtrans/emisell-extension.yaml" "${bundle_tmp}/bundle/emisell-extension.yaml"
install -m 0644 "${repo_dir}/provider-apps/midtrans/openapi.yaml" "${bundle_tmp}/bundle/openapi.yaml"
install -m 0644 "${repo_dir}/provider-apps/midtrans/README.md" "${bundle_tmp}/bundle/README.md"
install -m 0644 "${repo_dir}/provider-apps/midtrans/SECURITY.md" "${bundle_tmp}/bundle/SECURITY.md"
install -m 0644 "${repo_dir}/provider-apps/midtrans/contract-tests/README.md" "${bundle_tmp}/bundle/contract-tests/README.md"
install -m 0644 "${repo_dir}/backend/internal/midtrans/client.go" "${bundle_tmp}/bundle/src/midtrans/client.go"
install -m 0644 "${repo_dir}/backend/internal/midtrans/manifest.go" "${bundle_tmp}/bundle/src/midtrans/manifest.go"

(
  cd "${bundle_tmp}/bundle"
  touch -t 198001010000 emisell-extension.yaml openapi.yaml README.md SECURITY.md contract-tests/README.md src/midtrans/client.go src/midtrans/manifest.go
  COPYFILE_DISABLE=1 zip -X -q "${bundle_tmp}/midtrans-provider-app-emisell-v1.2.0.zip" \
    emisell-extension.yaml openapi.yaml README.md SECURITY.md contract-tests/README.md \
    src/midtrans/client.go src/midtrans/manifest.go
)

install -m 0644 "${bundle_tmp}/midtrans-provider-app-emisell-v1.2.0.zip" "${output_dir}/midtrans-provider-app-emisell-v1.2.0.zip"
shasum -a 256 "${output_dir}/midtrans-provider-app-emisell-v1.2.0.zip"
