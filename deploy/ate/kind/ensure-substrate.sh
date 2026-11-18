#!/usr/bin/env bash
# Clone/checkout Agent Substrate sources used by ate-setup.
set -euo pipefail
# shellcheck source=lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

need git
need go

if [[ ! -d "${ATE_SRC}/.git" ]]; then
  log "Cloning agent-substrate/substrate@${ATE_REF} → ${ATE_SRC}"
  mkdir -p "$(dirname "${ATE_SRC}")"
  git clone --depth 1 --branch "${ATE_REF}" https://github.com/agent-substrate/substrate.git "${ATE_SRC}"
else
  log "Using existing substrate checkout ${ATE_SRC}"
  git -C "${ATE_SRC}" fetch --depth 1 origin "refs/tags/${ATE_REF}:refs/tags/${ATE_REF}" 2>/dev/null || \
    git -C "${ATE_SRC}" fetch --depth 1 origin "${ATE_REF}" || true
  git -C "${ATE_SRC}" checkout "${ATE_REF}" >/dev/null
fi

need go
ensure_gopath_bin

log "Installing kubectl-ate plugin"
(cd "${ATE_SRC}" && go install ./cmd/kubectl-ate)
command -v kubectl-ate >/dev/null || {
  echo "kubectl-ate not on PATH after go install (expected under $(go env GOPATH)/bin)" >&2
  exit 1
}

log "Substrate ready at ${ATE_SRC}"
