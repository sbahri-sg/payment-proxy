#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_dir="${repo_dir}/dist/provider-apps"
bundle_tmp="$(mktemp -d "${TMPDIR:-/tmp}/emisell-midtrans-provider-app.XXXXXX")"
trap 'rm -rf "${bundle_tmp}"' EXIT

mkdir -p "${output_dir}" "${bundle_tmp}/runtime" "${bundle_tmp}/bundle"

docker buildx build \
  --file "${repo_dir}/backend/Dockerfile" \
  --target midtrans-provider-app-artifact \
  --output "type=local,dest=${bundle_tmp}/runtime" \
  "${repo_dir}/backend"

install -m 0755 "${bundle_tmp}/runtime/connector" "${bundle_tmp}/bundle/connector"
install -m 0644 "${repo_dir}/provider-apps/midtrans/manifest.json" "${bundle_tmp}/bundle/manifest.json"

(
  cd "${bundle_tmp}/bundle"
  shasum -a 256 manifest.json connector > checksums.txt
  COPYFILE_DISABLE=1 zip -X -q "${bundle_tmp}/midtrans-connector-emisell-v1.1.0.zip" manifest.json connector checksums.txt
)

install -m 0644 "${bundle_tmp}/midtrans-connector-emisell-v1.1.0.zip" "${output_dir}/midtrans-connector-emisell-v1.1.0.zip"
shasum -a 256 "${output_dir}/midtrans-connector-emisell-v1.1.0.zip"
