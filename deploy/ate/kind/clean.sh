#!/usr/bin/env bash
# Full cleanup of ate kind dogfood: cluster, registry, shims, generated files, local logs.
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "${DIR}/lib.sh"

log "Cleaning ate kind dogfood (${KIND_CLUSTER_NAME})"

# Cluster + registry (same as down.sh)
if command -v kind >/dev/null 2>&1; then
  kind_cmd delete cluster --name "${KIND_CLUSTER_NAME}" 2>/dev/null || true
else
  log "kind not installed; skip cluster delete"
fi

if command -v podman >/dev/null 2>&1 || command -v docker >/dev/null 2>&1; then
  container rm -f "${KIND_REGISTRY_NAME}" >/dev/null 2>&1 || true
fi

# Generated configs, docker→podman shim, local smoke.env is kept (user config)
rm -rf "${KIND_DIR}/.bin"
rm -f "${KIND_DIR}/.generated-kind-config.yaml" \
  "${KIND_DIR}/.generated-smoke-template.yaml" \
  "${KIND_DIR}/.generated-claude-template.yaml"

# Port-forward / proxy leftovers from smoke
rm -f /tmp/shuttle-ate-api-pf.log /tmp/shuttle-ate-proxy.log

# Optional: drop local podman images built for dogfood (ATE_KIND_CLEAN_IMAGES=1)
if [[ "${ATE_KIND_CLEAN_IMAGES:-}" == "1" ]] && command -v podman >/dev/null 2>&1; then
  log "Removing local actor images (ATE_KIND_CLEAN_IMAGES=1)"
  podman rmi -f "${SHUTTLE_ACTOR_IMAGE}" "${SHUTTLE_SMOKE_IMAGE}" >/dev/null 2>&1 || true
fi

log "ate kind clean complete"
