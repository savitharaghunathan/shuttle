#!/usr/bin/env bash
# Apply a WorkerPool so actors can resume.
set -euo pipefail
# shellcheck source=lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

need kubectl
ensure_docker_shim
kind_export_kubeconfig "${KIND_CLUSTER_NAME}"

resolve_worker_image() {
  if [[ -n "${ATE_WORKER_IMAGE}" ]]; then
    [[ "${ATE_WORKER_IMAGE}" == *@sha256:* ]] || {
      echo "ATE_WORKER_IMAGE must be a digest-pinned ref containing @sha256: (${ATE_WORKER_IMAGE})" >&2
      exit 1
    }
    echo "${ATE_WORKER_IMAGE}"
    return
  fi

  local ref="${ATE_IMAGE_REPO}/${ATE_WORKER_IMAGE_NAME}:${ATE_IMAGE_TAG}"
  local digest=""
  if command -v crane >/dev/null 2>&1; then
    digest="$(crane digest "${ref}")"
  else
    container pull "${ref}" >&2
    digest="$(container image inspect "${ref}" --format '{{.Digest}}')"
  fi
  [[ "${digest}" == sha256:* ]] || {
    echo "failed to resolve digest for ${ref}" >&2
    exit 1
  }
  echo "${ATE_IMAGE_REPO}/${ATE_WORKER_IMAGE_NAME}@${digest}"
}

log "Resolving ${ATE_IMAGE_REPO}/${ATE_WORKER_IMAGE_NAME}:${ATE_IMAGE_TAG}"
worker_ref="$(resolve_worker_image)"
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
