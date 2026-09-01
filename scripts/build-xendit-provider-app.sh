#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_dir="${repo_dir}/dist/provider-apps"
bundle_tmp="$(mktemp -d "${TMPDIR:-/tmp}/emisell-xendit-provider-app.XXXXXX")"
trap 'rm -rf "${bundle_tmp}"' EXIT

mkdir -p "${output_dir}" "${bundle_tmp}/bundle/contract-tests" "${bundle_tmp}/bundle/src/xendit"
install -m 0644 "${repo_dir}/provider-apps/xendit/emisell-extension.yaml" "${bundle_tmp}/bundle/emisell-extension.yaml"
install -m 0644 "${repo_dir}/provider-apps/xendit/openapi.yaml" "${bundle_tmp}/bundle/openapi.yaml"
install -m 0644 "${repo_dir}/provider-apps/xendit/README.md" "${bundle_tmp}/bundle/README.md"
install -m 0644 "${repo_dir}/provider-apps/xendit/SECURITY.md" "${bundle_tmp}/bundle/SECURITY.md"
install -m 0644 "${repo_dir}/provider-apps/xendit/contract-tests/README.md" "${bundle_tmp}/bundle/contract-tests/README.md"
install -m 0644 "${repo_dir}/backend/internal/xendit/client.go" "${bundle_tmp}/bundle/src/xendit/client.go"
install -m 0644 "${repo_dir}/backend/internal/xendit/manifest.go" "${bundle_tmp}/bundle/src/xendit/manifest.go"

(
  cd "${bundle_tmp}/bundle"
  touch -t 198001010000 emisell-extension.yaml openapi.yaml README.md SECURITY.md contract-tests/README.md src/xendit/client.go src/xendit/manifest.go
  COPYFILE_DISABLE=1 zip -X -q "${bundle_tmp}/xendit-provider-app-emisell-v1.zip" \
    emisell-extension.yaml openapi.yaml README.md SECURITY.md contract-tests/README.md \
    src/xendit/client.go src/xendit/manifest.go
)

install -m 0644 "${bundle_tmp}/xendit-provider-app-emisell-v1.zip" "${output_dir}/xendit-provider-app-emisell-v1.zip"
shasum -a 256 "${output_dir}/xendit-provider-app-emisell-v1.zip"
