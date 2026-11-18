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

# Egress is deny-by-default; without a policy, git clone cannot resolve/reach GitHub.
egress_file="${KIND_DIR}/.generated-smoke-egress.yaml"
cat >"${egress_file}" <<'EOF'
rules:
- https:
    hostnames: ["github.com", "*.github.com"]
- http:
    hostnames: ["github.com", "*.github.com"]
    ports:
      all: {}
EOF
log "Ensuring egress policy for ${ACTOR} (github.com)"
"${ATECTL[@]}" delete egress-policy "${ACTOR}" -a "${ATE_ATESPACE}" 2>/dev/null || true
"${ATECTL[@]}" create egress-policy "${ACTOR}" -a "${ATE_ATESPACE}" -f "${egress_file}"
# Gateway may cache a prior deny briefly.
sleep 2

log "Port-forward API + start actor proxy in background"
kubectl --context="${KUBECTL_CONTEXT}" -n ate-system port-forward svc/api 8443:443 >/tmp/shuttle-ate-api-pf.log 2>&1 &
API_PF=$!
# Workload listens on 8080 (not the proxy default remote port 80).
"${ATECTL[@]}" proxy actor "${ACTOR}" -a "${ATE_ATESPACE}" 8000:8080 >/tmp/shuttle-ate-proxy.log 2>&1 &
PROXY_PF=$!

cleanup() {
  kill "${API_PF}" "${PROXY_PF}" >/dev/null 2>&1 || true
  "${ATECTL[@]}" suspend actor "${ACTOR}" -a "${ATE_ATESPACE}" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

# Wait for proxy TCP (GET / may 404); fail if the proxy process dies.
ready=0
for _ in $(seq 1 60); do
  if ! kill -0 "${PROXY_PF}" 2>/dev/null; then
    echo "actor proxy exited early; see /tmp/shuttle-ate-proxy.log" >&2
    cat /tmp/shuttle-ate-proxy.log >&2 || true
    exit 1
  fi
  if (echo >/dev/tcp/127.0.0.1/8000) >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 1
done
[[ "${ready}" -eq 1 ]] || {
  echo "timed out waiting for actor proxy on :8000" >&2
  cat /tmp/shuttle-ate-proxy.log >&2 || true
  exit 1
}
sleep 1

TOKEN="${SMOKE_GIT_TOKEN:-}"
# Build JSON without leaking the token into shell history via set -x.
payload_file="$(mktemp)"
python3 - "${REPO_URL}" "${BRANCH}" "${TOKEN}" <<'PY' >"${payload_file}"
import json, sys
repo, branch, token = sys.argv[1], sys.argv[2], sys.argv[3]
body = {"prompt": "kind smoke", "branch": branch, "repo_url": repo}
if token:
    body["token"] = token
json.dump(body, sys.stdout)
PY

log "POST /v1/tasks"
http_code=0
resp=$(curl -sS --max-time 180 -o /tmp/shuttle-ate-smoke-resp.json -w '%{http_code}' -X POST "http://127.0.0.1:8000/v1/tasks" \
  -H "Content-Type: application/json" \
  -H "ate-target-actor: ${ATE_ATESPACE}/${ACTOR}" \
  --data-binary @"${payload_file}") || true
rm -f "${payload_file}"
http_code="${resp}"
body="$(cat /tmp/shuttle-ate-smoke-resp.json 2>/dev/null || true)"
echo "HTTP ${http_code}"
echo "${body}"
echo "${body}" | grep -q '"status":"succeeded"' || {
  echo "smoke failed: expected status=succeeded (HTTP ${http_code})" >&2
  echo "Hint: private repos need SMOKE_GIT_TOKEN (or GITHUB_TOKEN) in smoke.env; public HTTPS remotes work without a token." >&2
  exit 1
}

log "smoke passed"
