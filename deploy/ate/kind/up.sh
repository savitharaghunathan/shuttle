#!/usr/bin/env bash
# Bring up kind (podman) + ate + shuttle actor images/templates for local dogfood.
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "${DIR}/lib.sh"

need podman
need kind
need kubectl
need go
need git

log "1/5 create kind cluster (podman)"
"${DIR}/create-cluster.sh"

log "2/5 ensure Agent Substrate sources + kubectl-ate"
"${DIR}/ensure-substrate.sh"

log "3/5 install ate-system"
"${DIR}/install-ate.sh"

log "4/5 build + push actor images"
"${DIR}/build-images.sh"

log "5/5 apply actor templates"
"${DIR}/apply-templates.sh"

cat <<EOF

Cluster is up.

Next:
  cp deploy/ate/kind/smoke.env.example deploy/ate/kind/smoke.env
  # edit SMOKE_REPO_URL=https://github.com/OWNER/REPO.git
  make ate-kind-smoke
  make ate-kind-port-forward   # gRPC :8443 and task proxy :8000

Example shuttle.yaml ate block:
  ate:
    endpoint: "127.0.0.1:8443"
    atespace: ${ATE_ATESPACE}
    actor_template: ${ATE_SMOKE_TEMPLATE}   # or ${ATE_TEMPLATE} for Claude
    task_url: "http://127.0.0.1:8000"
    insecure: true
EOF
