#!/usr/bin/env bash
# Shared env for shuttle ate kind+podman local test.
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

# Smoke target repo: set via env or deploy/ate/kind/smoke.env (see smoke.env.example).
# No default remote — personal repos must not be hardcoded.
export SMOKE_BRANCH="${SMOKE_BRANCH:-shuttle/ate-kind-smoke}"
export SMOKE_ACTOR="${SMOKE_ACTOR:-shuttle-smoke-1}"
export SHUTTLE_TEST_APP="${SHUTTLE_TEST_APP:-}"

_load_smoke_env() {
  local f="${ATE_KIND_ENV:-${KIND_DIR}/smoke.env}"
  if [[ -f "${f}" ]]; then
    # shellcheck disable=SC1090
    set -a
    # shellcheck disable=SC1090
    source "${f}"
    set +a
  fi
}
_load_smoke_env
# Re-apply defaults after sourcing so empty file keys don't unset intentional exports.
export SMOKE_BRANCH="${SMOKE_BRANCH:-shuttle/ate-kind-smoke}"
export SMOKE_ACTOR="${SMOKE_ACTOR:-shuttle-smoke-1}"
export SMOKE_REPO_URL="${SMOKE_REPO_URL:-}"
export SHUTTLE_TEST_APP="${SHUTTLE_TEST_APP:-}"

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

# ate-setup calls `docker info` and other docker CLI entrypoints. For podman
# kind installs, prepend a shim so those calls hit podman instead.
ensure_docker_shim() {
  if [[ "${KIND_EXPERIMENTAL_PROVIDER}" != "podman" ]]; then
    return 0
  fi
  local shim_dir="${KIND_DIR}/.bin"
  mkdir -p "${shim_dir}"
  # Named exactly `docker` so exec.LookPath("docker") finds it.
  cp "${KIND_DIR}/docker-shim.sh" "${shim_dir}/docker"
  chmod +x "${shim_dir}/docker"
  export PATH="${shim_dir}:${PATH}"
  if ! docker info >/dev/null 2>&1; then
    echo "podman docker-shim failed \`docker info\`. Is the podman machine running?" >&2
    echo "  podman machine start" >&2
    docker info >&2 || true
    exit 1
  fi
  log "Using podman via PATH docker shim (${shim_dir}/docker)"
}
