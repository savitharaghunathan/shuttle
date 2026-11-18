#!/usr/bin/env bash
# Install Agent Substrate control plane into the current kind cluster.
set -euo pipefail
# shellcheck source=lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

need go
need kubectl
[[ -d "${ATE_SRC}" ]] || { echo "run ensure-substrate.sh first" >&2; exit 1; }

export ATE_INSTALL_KIND=true
export KO_DOCKER_REPO
export KIND_EXPERIMENTAL_PROVIDER

log "Deploying ate-system (this can take several minutes)"
(
  cd "${ATE_SRC}"
  # Prefer a credential provider that works for local kind dogfood.
  go run ./cmd/ate-setup deploy ate-system \
    --credential-provider='{"name":"k8s.io"}' \
    --ingress-allowed-spiffe-ids=spiffe://cluster.local/ns/ate-system/sa/atenet-client
)

log "Waiting for ate-api-server"
kubectl --context="${KUBECTL_CONTEXT}" -n ate-system rollout status deploy/ate-api-server --timeout=10m

log "ate-system installed"
kubectl --context="${KUBECTL_CONTEXT}" -n ate-system get pods
