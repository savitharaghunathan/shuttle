#!/usr/bin/env bash
# Build shuttle actor images with podman (Containerfile) and push to kind registry.
# Writes digest-pinned refs to .generated-image-refs.env (ate requires @sha256:…).
set -euo pipefail
# shellcheck source=lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

need podman
ensure_docker_shim

refs_file="${KIND_DIR}/.generated-image-refs.env"
: >"${refs_file}"

# Always build as registry/name:latest so host:port colons are not confused with tags.
build_and_push() {
  local containerfile="$1"
  local context="$2"
  local image_name="$3" # e.g. shuttle-smoke-actor
  local varname="$4"
  local tag="${KO_DOCKER_REPO}/${image_name}:latest"
  local digest_file pinned digest

  log "Building ${tag} from ${containerfile}"
  podman build -f "${containerfile}" -t "${tag}" "${context}"
  digest_file="$(mktemp)"
  # Push via 127.0.0.1 (macOS localhost may be ::1); keep localhost in the pinned ref
  # so kind's containerd localhost:PORT mirror matches.
  podman push --tls-verify=false --digestfile="${digest_file}" "${tag}" \
    "docker://127.0.0.1:${KIND_REGISTRY_PORT}/${image_name}:latest"
  digest="$(tr -d '[:space:]' <"${digest_file}")"
  rm -f "${digest_file}"
  [[ -n "${digest}" ]] || { echo "empty digest for ${tag}" >&2; exit 1; }
  pinned="${KO_DOCKER_REPO}/${image_name}@${digest}"
  printf '%s=%q\n' "${varname}" "${pinned}" >>"${refs_file}"
  export "${varname}=${pinned}"
  echo "  ${varname}=${pinned}"
}

build_and_push \
  "${ATE_DIR}/workload/Containerfile" \
  "${ATE_DIR}/workload" \
  shuttle-claude-actor \
  SHUTTLE_ACTOR_IMAGE

build_and_push \
  "${ATE_DIR}/kind/smoke-workload/Containerfile" \
  "${ATE_DIR}/kind/smoke-workload" \
  shuttle-smoke-actor \
  SHUTTLE_SMOKE_IMAGE

log "Pushed (digest-pinned refs in ${refs_file}):"
# shellcheck disable=SC1090
source "${refs_file}"
echo "  ${SHUTTLE_ACTOR_IMAGE}"
echo "  ${SHUTTLE_SMOKE_IMAGE}"
