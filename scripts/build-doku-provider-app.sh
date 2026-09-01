#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_dir="${repo_dir}/dist/provider-apps"
bundle_tmp="$(mktemp -d "${TMPDIR:-/tmp}/emisell-doku-provider-app.XXXXXX")"
trap 'rm -rf "${bundle_tmp}"' EXIT

mkdir -p "${output_dir}" "${bundle_tmp}/bundle/contract-tests" "${bundle_tmp}/bundle/src/doku"
install -m 0644 "${repo_dir}/provider-apps/doku/emisell-extension.yaml" "${bundle_tmp}/bundle/emisell-extension.yaml"
install -m 0644 "${repo_dir}/provider-apps/doku/openapi.yaml" "${bundle_tmp}/bundle/openapi.yaml"
install -m 0644 "${repo_dir}/provider-apps/doku/README.md" "${bundle_tmp}/bundle/README.md"
install -m 0644 "${repo_dir}/provider-apps/doku/SECURITY.md" "${bundle_tmp}/bundle/SECURITY.md"
install -m 0644 "${repo_dir}/provider-apps/doku/contract-tests/README.md" "${bundle_tmp}/bundle/contract-tests/README.md"
install -m 0644 "${repo_dir}/backend/internal/doku/client.go" "${bundle_tmp}/bundle/src/doku/client.go"
install -m 0644 "${repo_dir}/backend/internal/doku/manifest.go" "${bundle_tmp}/bundle/src/doku/manifest.go"

(
  cd "${bundle_tmp}/bundle"
  touch -t 198001010000 emisell-extension.yaml openapi.yaml README.md SECURITY.md contract-tests/README.md src/doku/client.go src/doku/manifest.go
  COPYFILE_DISABLE=1 zip -X -q "${bundle_tmp}/doku-provider-app-emisell-v1.0.0.zip" \
    emisell-extension.yaml openapi.yaml README.md SECURITY.md contract-tests/README.md \
    src/doku/client.go src/doku/manifest.go
)

install -m 0644 "${bundle_tmp}/doku-provider-app-emisell-v1.0.0.zip" "${output_dir}/doku-provider-app-emisell-v1.0.0.zip"
shasum -a 256 "${output_dir}/doku-provider-app-emisell-v1.0.0.zip"
