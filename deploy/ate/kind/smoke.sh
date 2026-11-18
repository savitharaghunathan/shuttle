#!/usr/bin/env bash
# Smoke: resume smoke actor, POST /v1/tasks, expect succeeded JSON.
# Requires: make ate-kind-up, and a reachable git repo URL (SMOKE_REPO_URL).
set -euo pipefail
# shellcheck source=lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

need kubectl
need curl
need go
ensure_gopath_bin
kind_export_kubeconfig "${KIND_CLUSTER_NAME}"

ACTOR="${SMOKE_ACTOR}"
BRANCH="${SMOKE_BRANCH}"
REPO_URL="${SMOKE_REPO_URL:-}"
if [[ -z "${REPO_URL}" ]]; then
  cat >&2 <<'EOF'
SMOKE_REPO_URL is required (HTTPS git remote the actor can clone/push).

  cp deploy/ate/kind/smoke.env.example deploy/ate/kind/smoke.env
  # edit SMOKE_REPO_URL=https://github.com/OWNER/REPO.git
  make ate-kind-smoke

Or: SMOKE_REPO_URL=https://github.com/OWNER/REPO.git make ate-kind-smoke
EOF
  exit 1
fi
log "Using smoke repo ${REPO_URL}"
if [[ -n "${SHUTTLE_TEST_APP}" ]]; then
  log "Local checkout hint: ${SHUTTLE_TEST_APP}"
fi

ATECTL=(kubectl ate --context "${KUBECTL_CONTEXT}")
if ! kubectl ate --help >/dev/null 2>&1; then
  if command -v kubectl-ate >/dev/null; then
    ATECTL=(kubectl-ate --context "${KUBECTL_CONTEXT}")
  else
    echo "kubectl-ate plugin missing; run make ate-kind-up / ensure-substrate.sh" >&2
    exit 1
  fi
fi

log "Create/resume smoke actor ${ACTOR}"
"${ATECTL[@]}" create actor "${ACTOR}" -a "${ATE_ATESPACE}" --template "${ATE_SMOKE_TEMPLATE}" 2>/dev/null || true
"${ATECTL[@]}" resume actor "${ACTOR}" -a "${ATE_ATESPACE}"

log "Port-forward API + start actor proxy in background"
kubectl --context="${KUBECTL_CONTEXT}" -n ate-system port-forward svc/api 8443:443 >/tmp/shuttle-ate-api-pf.log 2>&1 &
API_PF=$!
"${ATECTL[@]}" proxy actor "${ACTOR}" -a "${ATE_ATESPACE}" --port 8000 >/tmp/shuttle-ate-proxy.log 2>&1 &
PROXY_PF=$!

cleanup() {
  kill "${API_PF}" "${PROXY_PF}" >/dev/null 2>&1 || true
  "${ATECTL[@]}" suspend actor "${ACTOR}" -a "${ATE_ATESPACE}" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

# Wait for proxy
for _ in $(seq 1 60); do
  if curl -sf -o /dev/null -w '' http://127.0.0.1:8000/ 2>/dev/null || true; then
    break
  fi
  # proxy may 404 on GET /; try until TCP accepts
  if (echo >/dev/tcp/127.0.0.1/8000) >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
sleep 2

log "POST /v1/tasks"
resp=$(curl -sf -X POST "http://127.0.0.1:8000/v1/tasks" \
  -H "Content-Type: application/json" \
  -H "ate-target-actor: ${ATE_ATESPACE}/${ACTOR}" \
  -d "{\"prompt\":\"kind smoke\",\"branch\":\"${BRANCH}\",\"repo_url\":\"${REPO_URL}\"}")

echo "${resp}"
echo "${resp}" | grep -q '"status":"succeeded"' || {
  echo "smoke failed: expected status=succeeded" >&2
  exit 1
}

log "smoke passed"
