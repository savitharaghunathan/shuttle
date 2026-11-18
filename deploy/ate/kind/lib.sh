#!/usr/bin/env bash
# Shared env for shuttle ate kind+podman dogfood.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
ATE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KIND_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

export KIND_EXPERIMENTAL_PROVIDER="${KIND_EXPERIMENTAL_PROVIDER:-podman}"
export KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-shuttle-ate}"
export KIND_REGISTRY_NAME="${KIND_REGISTRY_NAME:-kind-registry}"
export KIND_REGISTRY_PORT="${KIND_REGISTRY_PORT:-5001}"
export KUBECTL_CONTEXT="kind-${KIND_CLUSTER_NAME}"
export ATE_SRC="${ATE_SRC:-${HOME}/.cache/shuttle/agent-substrate}"
export ATE_REF="${ATE_REF:-v1.0.0}"
export ATE_INSTALL_KIND=true
export KO_DOCKER_REPO="${KO_DOCKER_REPO:-localhost:${KIND_REGISTRY_PORT}}"
export SHUTTLE_ACTOR_IMAGE="${SHUTTLE_ACTOR_IMAGE:-${KO_DOCKER_REPO}/shuttle-claude-actor:latest}"
export SHUTTLE_SMOKE_IMAGE="${SHUTTLE_SMOKE_IMAGE:-${KO_DOCKER_REPO}/shuttle-smoke-actor:latest}"
export ATE_ATESPACE="${ATE_ATESPACE:-shuttle}"
export ATE_TEMPLATE="${ATE_TEMPLATE:-shuttle-claude}"
export ATE_SMOKE_TEMPLATE="${ATE_SMOKE_TEMPLATE:-shuttle-smoke}"

# Default dogfood target: Savitha's local shuttle test app / GitHub repo.
export SHUTTLE_TEST_APP="${SHUTTLE_TEST_APP:-${HOME}/local_dev/test}"
export SMOKE_REPO_URL="${SMOKE_REPO_URL:-https://github.com/savitharaghunathan/test.git}"
export SMOKE_BRANCH="${SMOKE_BRANCH:-shuttle/ate-kind-smoke}"
export SMOKE_ACTOR="${SMOKE_ACTOR:-shuttle-smoke-1}"

container() {
  # Prefer podman; fall back to docker only if explicitly requested.
  if [[ "${KIND_EXPERIMENTAL_PROVIDER}" == "podman" ]]; then
    command -v podman >/dev/null || { echo "podman required (KIND_EXPERIMENTAL_PROVIDER=podman)" >&2; exit 1; }
    podman "$@"
  else
    command -v docker >/dev/null || { echo "docker required" >&2; exit 1; }
    docker "$@"
  fi
}

kind_cmd() {
  command -v kind >/dev/null || { echo "kind required: https://kind.sigs.k8s.io/" >&2; exit 1; }
  KIND_EXPERIMENTAL_PROVIDER="${KIND_EXPERIMENTAL_PROVIDER}" kind "$@"
}

need() {
  command -v "$1" >/dev/null || { echo "missing required tool: $1" >&2; exit 1; }
}

log() { printf '==> %s\n' "$*"; }
