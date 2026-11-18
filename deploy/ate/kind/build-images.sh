#!/usr/bin/env bash
# Build shuttle actor images with podman (Containerfile) and push to kind registry.
# Writes digest-pinned refs to .generated-image-refs.env (ate requires @sha256:…).
set -euo pipefail
# shellcheck source=lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

need podman
ensure_docker_shim

refs_file="${KIND_DIR}/.generated-image-refs.env"
: >"${refs_file}"

build_and_push() {
  local containerfile="$1"
  local context="$2"
  local tag="$3"
  local varname="$4"
  local digest_file pinned repo
  log "Building ${tag} from ${containerfile}"
  podman build -f "${containerfile}" -t "${tag}" "${context}"
  digest_file="$(mktemp)"
  podman push --tls-verify=false --digestfile="${digest_file}" "${tag}"
  digest="$(tr -d '[:space:]' <"${digest_file}")"
  rm -f "${digest_file}"
  [[ -n "${digest}" ]] || { echo "empty digest for ${tag}" >&2; exit 1; }
  repo="${tag%%:*}"
  pinned="${repo}@${digest}"
  printf '%s=%q\n' "${varname}" "${pinned}" >>"${refs_file}"
  # Export for same-shell callers; apply-templates sources the file.
  export "${varname}=${pinned}"
  echo "  ${varname}=${pinned}"
}

build_and_push \
  "${ATE_DIR}/workload/Containerfile" \
  "${ATE_DIR}/workload" \
  "${SHUTTLE_ACTOR_IMAGE%%@*}" \
  SHUTTLE_ACTOR_IMAGE

build_and_push \
  "${ATE_DIR}/kind/smoke-workload/Containerfile" \
  "${ATE_DIR}/kind/smoke-workload" \
  "${SHUTTLE_SMOKE_IMAGE%%@*}" \
  SHUTTLE_SMOKE_IMAGE

log "Pushed (digest-pinned refs in ${refs_file}):"
# shellcheck disable=SC1090
source "${refs_file}"
echo "  ${SHUTTLE_ACTOR_IMAGE}"
echo "  ${SHUTTLE_SMOKE_IMAGE}"
