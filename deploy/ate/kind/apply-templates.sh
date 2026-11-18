#!/usr/bin/env bash
# Create atespace + smoke/claude actor templates via kubectl-ate.
set -euo pipefail
# shellcheck source=lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

need kubectl
need go
ensure_gopath_bin

ATECTL=(kubectl ate)
if ! kubectl ate --help >/dev/null 2>&1; then
  if command -v kubectl-ate >/dev/null; then
    ATECTL=(kubectl-ate)
  else
    echo "kubectl-ate plugin missing; run ensure-substrate.sh (installs to $(go env GOPATH)/bin)" >&2
    exit 1
  fi
fi

log "Ensuring atespace ${ATE_ATESPACE}"
"${ATECTL[@]}" create atespace "${ATE_ATESPACE}" 2>/dev/null || true

smoke_tmpl="${KIND_DIR}/.generated-smoke-template.yaml"
claude_tmpl="${KIND_DIR}/.generated-claude-template.yaml"

cat >"${smoke_tmpl}" <<EOF
metadata:
  atespace: ${ATE_ATESPACE}
  name: ${ATE_SMOKE_TEMPLATE}
containers:
  - name: agent
    image:
      reference: ${SHUTTLE_SMOKE_IMAGE}
    ports:
      - name: http
        containerPort: 8080
resources:
  limits:
    cpuMillicores: 500
    memoryBytes: "536870912"
snapshotConfig:
  preferredFidelity: SNAPSHOT_FIDELITY_MEMORY
EOF

cat >"${claude_tmpl}" <<EOF
metadata:
  atespace: ${ATE_ATESPACE}
  name: ${ATE_TEMPLATE}
containers:
  - name: agent
    image:
      reference: ${SHUTTLE_ACTOR_IMAGE}
    ports:
      - name: http
        containerPort: 8080
resources:
  limits:
    cpuMillicores: 2000
    memoryBytes: "4294967296"
snapshotConfig:
  preferredFidelity: SNAPSHOT_FIDELITY_MEMORY
EOF

log "Creating actor templates (delete+recreate if immutable)"
"${ATECTL[@]}" delete actor-template "${ATE_SMOKE_TEMPLATE}" -a "${ATE_ATESPACE}" 2>/dev/null || true
"${ATECTL[@]}" delete actor-template "${ATE_TEMPLATE}" -a "${ATE_ATESPACE}" 2>/dev/null || true
"${ATECTL[@]}" create actor-template -f "${smoke_tmpl}"
"${ATECTL[@]}" create actor-template -f "${claude_tmpl}"

log "Templates ready: ${ATE_SMOKE_TEMPLATE}, ${ATE_TEMPLATE}"
