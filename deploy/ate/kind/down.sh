#!/usr/bin/env bash
# Tear down kind cluster + local registry created for ate dogfood.
set -euo pipefail
# shellcheck source=lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

log "Deleting kind cluster ${KIND_CLUSTER_NAME}"
kind_cmd delete cluster --name "${KIND_CLUSTER_NAME}" 2>/dev/null || true

log "Removing registry ${KIND_REGISTRY_NAME}"
container rm -f "${KIND_REGISTRY_NAME}" >/dev/null 2>&1 || true

rm -f "${KIND_DIR}/.generated-kind-config.yaml" \
  "${KIND_DIR}/.generated-smoke-template.yaml" \
  "${KIND_DIR}/.generated-claude-template.yaml"

log "Down complete"
