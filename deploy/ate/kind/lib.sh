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
# Kind nodes pull via the localhost:PORT containerd mirror. Never use 127.0.0.1
# here — inside a node that address is the node itself, not the registry.
export KO_DOCKER_REPO="localhost:${KIND_REGISTRY_PORT}"
export SHUTTLE_ACTOR_IMAGE="${KO_DOCKER_REPO}/shuttle-claude-actor:latest"
export SHUTTLE_SMOKE_IMAGE="${KO_DOCKER_REPO}/shuttle-smoke-actor:latest"
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
# Optional PAT for private HTTPS remotes (also accepts GITHUB_TOKEN).
export SMOKE_GIT_TOKEN="${SMOKE_GIT_TOKEN:-${GITHUB_TOKEN:-}}"
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

# kind v0.32 + podman 6.x breaks `kind get clusters` / export kubeconfig:
# podman ps --format '{{index .Labels "…"}}' errors because Labels is not a map
# in that template. Derive cluster names from label strings instead.
kind_cluster_names() {
  if [[ "${KIND_EXPERIMENTAL_PROVIDER}" == "podman" ]]; then
    podman ps -a --filter label=io.x-k8s.kind.cluster --format '{{.Labels}}' 2>/dev/null \
      | sed -n 's/.*io\.x-k8s\.kind\.cluster=\([^,]*\).*/\1/p' \
      | sort -u
    return 0
  fi
  kind_cmd get clusters 2>/dev/null || true
}

kind_cluster_exists() {
  local name="$1"
  kind_cluster_names | grep -qx "${name}"
}

# Write/merge kubeconfig for a kind cluster. Prefer kind export; on podman
# template failure, rebuild from the control-plane admin.conf + published port.
kind_export_kubeconfig() {
  local name="${1:-${KIND_CLUSTER_NAME}}"
  local context="kind-${name}"
  if kind_cmd export kubeconfig --name "${name}" >/dev/null 2>&1; then
    kubectl config use-context "${context}" >/dev/null 2>&1 || true
    return 0
  fi
  if [[ "${KIND_EXPERIMENTAL_PROVIDER}" != "podman" ]]; then
    echo "kind export kubeconfig failed for ${name}" >&2
    return 1
  fi
  local node port tmp merged
  node="$(podman ps -a --filter "label=io.x-k8s.kind.cluster=${name}" \
    --filter "label=io.x-k8s.kind.role=control-plane" --format '{{.Names}}' | head -1)"
  [[ -n "${node}" ]] || { echo "no control-plane container for kind cluster ${name}" >&2; return 1; }
  port="$(podman port "${node}" 6443/tcp | head -1 | awk -F: '{print $NF}')"
  [[ -n "${port}" ]] || { echo "no published 6443 for ${node}" >&2; return 1; }
  tmp="$(mktemp)"
  merged="$(mktemp)"
  podman exec "${node}" cat /etc/kubernetes/admin.conf >"${tmp}"
  # Point at the published localhost port and rename to kind-<name> context.
  sed -E \
    -e "s#server: https://.*#server: https://127.0.0.1:${port}#" \
    -e "s#kubernetes-admin@${name}#${context}#g" \
    -e "s#(name|cluster): ${name}\$#\\1: ${context}#g" \
    -e "s#user: kubernetes-admin\$#user: ${context}#" \
    -e "s#- name: kubernetes-admin\$#- name: ${context}#" \
    -e "s#current-context: .*#current-context: ${context}#" \
    "${tmp}" >"${tmp}.sed"
  mv "${tmp}.sed" "${tmp}"
  mkdir -p "${HOME}/.kube"
  if [[ -f "${HOME}/.kube/config" ]]; then
    KUBECONFIG="${HOME}/.kube/config:${tmp}" kubectl config view --flatten >"${merged}"
    mv "${merged}" "${HOME}/.kube/config"
  else
    mv "${tmp}" "${HOME}/.kube/config"
    rm -f "${merged}"
  fi
  rm -f "${tmp}" "${merged}"
  kubectl config use-context "${context}" >/dev/null
  log "Restored kubeconfig context ${context} via podman (kind export unavailable)"
}

need() {
  command -v "$1" >/dev/null || { echo "missing required tool: $1" >&2; exit 1; }
}

log() { printf '==> %s\n' "$*"; }

# go install lands in $(go env GOPATH)/bin (or GOBIN). Many interactive shells
# omit that directory from PATH; ate kind scripts must still find kubectl-ate.
ensure_gopath_bin() {
  local gobin gopath bin
  gobin="$(go env GOBIN 2>/dev/null || true)"
  gopath="$(go env GOPATH 2>/dev/null || true)"
  if [[ -n "${gobin}" ]]; then
    bin="${gobin}"
  elif [[ -n "${gopath}" ]]; then
    bin="${gopath}/bin"
  else
    return 0
  fi
  case ":${PATH}:" in
    *":${bin}:"*) ;;
    *) export PATH="${bin}:${PATH}" ;;
  esac
}

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
