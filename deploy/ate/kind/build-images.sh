#!/usr/bin/env bash
# Build shuttle actor images with podman (Containerfile) and push to kind registry.
set -euo pipefail
# shellcheck source=lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

need podman

build_and_push() {
  local containerfile="$1"
  local context="$2"
  local tag="$3"
  log "Building ${tag} from ${containerfile}"
  podman build -f "${containerfile}" -t "${tag}" "${context}"
  podman push --tls-verify=false "${tag}"
}

build_and_push \
  "${ATE_DIR}/workload/Containerfile" \
  "${ATE_DIR}/workload" \
  "${SHUTTLE_ACTOR_IMAGE}"

build_and_push \
  "${ATE_DIR}/kind/smoke-workload/Containerfile" \
  "${ATE_DIR}/kind/smoke-workload" \
  "${SHUTTLE_SMOKE_IMAGE}"

log "Pushed:"
echo "  ${SHUTTLE_ACTOR_IMAGE}"
echo "  ${SHUTTLE_SMOKE_IMAGE}"
