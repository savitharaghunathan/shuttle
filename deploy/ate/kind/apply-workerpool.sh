#!/usr/bin/env bash
# Build ateworker-gvisor and apply a WorkerPool so actors can resume.
set -euo pipefail
# shellcheck source=lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

need kubectl
need go
ensure_gopath_bin
ensure_docker_shim
kind_export_kubeconfig "${KIND_CLUSTER_NAME}"

if ! command -v ko >/dev/null 2>&1; then
  log "Installing ko"
  go install github.com/google/ko@latest
  ensure_gopath_bin
fi

# Use a path under the registry (not --bare): ko rejects "localhost:5001" as a
# bare repository name because of the port colon.
ko_repo="localhost:${KIND_REGISTRY_PORT}"
log "Building ateworker-gvisor → ${ko_repo}/ateworker-gvisor"
worker_ref="$(
  cd "${ATE_SRC}"
  KO_DOCKER_REPO="${ko_repo}" \
  KO_DEFAULTPLATFORMS="${KO_DEFAULTPLATFORMS:-linux/arm64}" \
    ko build go.ate.dev/substrate/cmd/ateworker-gvisor
)"
worker_ref="$(echo "${worker_ref}" | tr -d '[:space:]')"
[[ "${worker_ref}" == *@sha256:* ]] || {
  echo "ko build did not return a digest-pinned ref: ${worker_ref}" >&2
  exit 1
}
log "Worker image ${worker_ref}"

refs_file="${KIND_DIR}/.generated-image-refs.env"
if [[ -f "${refs_file}" ]]; then
  grep -v '^SHUTTLE_WORKER_IMAGE=' "${refs_file}" >"${refs_file}.tmp" || true
  mv "${refs_file}.tmp" "${refs_file}"
fi
printf 'SHUTTLE_WORKER_IMAGE=%q\n' "${worker_ref}" >>"${refs_file}"

out="${KIND_DIR}/.generated-workerpool.yaml"
sed "s|WORKER_IMAGE_REPLACE_ME|${worker_ref}|g" "${KIND_DIR}/workerpool.yaml" >"${out}"

log "Applying WorkerPool"
kubectl --context="${KUBECTL_CONTEXT}" apply -f "${out}"

# WorkerPool controller creates a Deployment named after the pool.
log "Waiting for worker Deployment"
kubectl --context="${KUBECTL_CONTEXT}" -n shuttle rollout status deploy/shuttle --timeout=5m

log "WorkerPool ready"
kubectl --context="${KUBECTL_CONTEXT}" -n shuttle get pods,workerpool
