#!/usr/bin/env bash
# Port-forward ate-api-server (:8443) and start kubectl ate proxy for an actor (:8000).
set -euo pipefail
# shellcheck source=lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

need kubectl
need go
ensure_gopath_bin
kind_export_kubeconfig "${KIND_CLUSTER_NAME}"

ACTOR="${1:-shuttle-smoke-1}"
TEMPLATE="${2:-${ATE_SMOKE_TEMPLATE}}"

ATECTL=(kubectl ate --context "${KUBECTL_CONTEXT}")
if ! kubectl ate --help >/dev/null 2>&1; then
  if command -v kubectl-ate >/dev/null; then
    ATECTL=(kubectl-ate --context "${KUBECTL_CONTEXT}")
  else
    echo "kubectl-ate plugin missing; run make ate-kind-up / ensure-substrate.sh" >&2
    exit 1
  fi
fi

log "Ensuring actor ${ACTOR} from template ${TEMPLATE}"
"${ATECTL[@]}" create actor "${ACTOR}" -a "${ATE_ATESPACE}" --template "${TEMPLATE}" 2>/dev/null || true

log "Port-forward ate-api-server → 127.0.0.1:8443"
kubectl --context="${KUBECTL_CONTEXT}" -n ate-system port-forward svc/api 8443:443 >/tmp/shuttle-ate-api-pf.log 2>&1 &
API_PF=$!

cleanup() {
  kill "${API_PF}" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

sleep 1
log "Proxying actor ${ACTOR} → 127.0.0.1:8000 (blocks; Ctrl-C to stop)"
"${ATECTL[@]}" proxy actor "${ACTOR}" -a "${ATE_ATESPACE}" --port 8000
