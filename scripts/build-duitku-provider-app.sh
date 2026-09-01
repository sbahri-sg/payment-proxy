#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_dir="${repo_dir}/dist/provider-apps"
bundle_tmp="$(mktemp -d "${TMPDIR:-/tmp}/emisell-duitku-provider-app.XXXXXX")"
trap 'rm -rf "${bundle_tmp}"' EXIT

mkdir -p "${output_dir}" "${bundle_tmp}/bundle/contract-tests" "${bundle_tmp}/bundle/src/duitku"
install -m 0644 "${repo_dir}/provider-apps/duitku/emisell-extension.yaml" "${bundle_tmp}/bundle/emisell-extension.yaml"
install -m 0644 "${repo_dir}/provider-apps/duitku/openapi.yaml" "${bundle_tmp}/bundle/openapi.yaml"
install -m 0644 "${repo_dir}/provider-apps/duitku/README.md" "${bundle_tmp}/bundle/README.md"
install -m 0644 "${repo_dir}/provider-apps/duitku/SECURITY.md" "${bundle_tmp}/bundle/SECURITY.md"
install -m 0644 "${repo_dir}/provider-apps/duitku/contract-tests/README.md" "${bundle_tmp}/bundle/contract-tests/README.md"
install -m 0644 "${repo_dir}/backend/internal/duitku/client.go" "${bundle_tmp}/bundle/src/duitku/client.go"
install -m 0644 "${repo_dir}/backend/internal/duitku/manifest.go" "${bundle_tmp}/bundle/src/duitku/manifest.go"

(
  cd "${bundle_tmp}/bundle"
  touch -t 198001010000 emisell-extension.yaml openapi.yaml README.md SECURITY.md contract-tests/README.md src/duitku/client.go src/duitku/manifest.go
  COPYFILE_DISABLE=1 zip -X -q "${bundle_tmp}/duitku-provider-app-emisell-v2.0.2.zip" \
    emisell-extension.yaml openapi.yaml README.md SECURITY.md contract-tests/README.md \
    src/duitku/client.go src/duitku/manifest.go
)

install -m 0644 "${bundle_tmp}/duitku-provider-app-emisell-v2.0.2.zip" "${output_dir}/duitku-provider-app-emisell-v2.0.2.zip"
shasum -a 256 "${output_dir}/duitku-provider-app-emisell-v2.0.2.zip"
