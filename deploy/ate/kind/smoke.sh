#!/usr/bin/env bash
# Smoke: resume smoke actor, POST /v1/tasks, expect succeeded JSON.
# Requires: make ate-kind-up, and a reachable git repo URL (SMOKE_REPO_URL).
set -euo pipefail
# shellcheck source=lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

need kubectl
need curl

ACTOR="${SMOKE_ACTOR}"
BRANCH="${SMOKE_BRANCH}"
REPO_URL="${SMOKE_REPO_URL}"
if [[ -z "${REPO_URL}" ]]; then
  echo "SMOKE_REPO_URL is empty (expected https://github.com/savitharaghunathan/test.git)." >&2
  exit 1
fi
log "Using test repo ${REPO_URL} (local checkout: ${SHUTTLE_TEST_APP})"

ATECTL=(kubectl ate)
kubectl ate --help >/dev/null 2>&1 || ATECTL=(kubectl-ate)

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
