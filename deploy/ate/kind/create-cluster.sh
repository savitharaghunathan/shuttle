#!/usr/bin/env bash
# Create a kind cluster + local registry using podman (not docker).
# Mirrors Agent Substrate's hack/create-kind-cluster.sh shape for gVisor local test.
set -euo pipefail
# shellcheck source=lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

need kind
need kubectl

reg_name="${KIND_REGISTRY_NAME}"
reg_port="${KIND_REGISTRY_PORT}"
cfg="${KIND_DIR}/.generated-kind-config.yaml"
mkdir -p "${KIND_DIR}"

log "Using container runtime: ${KIND_EXPERIMENTAL_PROVIDER}"

# 1. Local registry
if container inspect -f '{{.State.Running}}' "${reg_name}" 2>/dev/null | grep -q true; then
  log "Registry ${reg_name} already running"
else
  log "Starting local registry ${reg_name} on 127.0.0.1:${reg_port}"
  container rm -f "${reg_name}" >/dev/null 2>&1 || true
  container run -d --restart=always \
    --name "${reg_name}" \
    -p "127.0.0.1:${reg_port}:5000" \
    registry:2
fi

# 2. kind config (gVisor-friendly; skip KVM/microVM on typical Mac podman)
log "Writing kind config to ${cfg}"
cat >"${cfg}" <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
featureGates:
  ClusterTrustBundle: true
  ClusterTrustBundleProjection: true
  PodCertificateRequest: true
runtimeConfig:
  "certificates.k8s.io/v1beta1": "true"
networking:
  ipFamily: ipv4
kubeadmConfigPatches:
- |
  kind: KubeletConfiguration
  serializeImagePulls: false
  maxParallelImagePulls: 4
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry]
    config_path = "/etc/containerd/certs.d"
EOF

if kind_cluster_exists "${KIND_CLUSTER_NAME}"; then
  log "Deleting existing cluster ${KIND_CLUSTER_NAME}"
  # kind delete also uses the broken Labels template on some podman versions;
  # remove control-plane/worker containers directly when kind fails.
  if ! kind_cmd delete cluster --name "${KIND_CLUSTER_NAME}"; then
    log "kind delete failed; removing containers labeled io.x-k8s.kind.cluster=${KIND_CLUSTER_NAME}"
    while read -r c; do
      [[ -n "${c}" ]] || continue
      container rm -f "${c}" >/dev/null 2>&1 || true
    done < <(container ps -a --filter "label=io.x-k8s.kind.cluster=${KIND_CLUSTER_NAME}" --format '{{.Names}}')
  fi
fi

log "Creating kind cluster ${KIND_CLUSTER_NAME}"
kind_cmd create cluster --name "${KIND_CLUSTER_NAME}" --config "${cfg}"
kind_export_kubeconfig "${KIND_CLUSTER_NAME}"

# 3. Registry mirror on nodes
REGISTRY_DIR="/etc/containerd/certs.d/localhost:${reg_port}"
nodes="$(kind_cmd get nodes --name "${KIND_CLUSTER_NAME}" 2>/dev/null || true)"
if [[ -z "${nodes}" ]]; then
  nodes="$(container ps -a --filter "label=io.x-k8s.kind.cluster=${KIND_CLUSTER_NAME}" --format '{{.Names}}')"
fi
for node in ${nodes}; do
  container exec "${node}" mkdir -p "${REGISTRY_DIR}"
  cat <<EOF | container exec -i "${node}" cp /dev/stdin "${REGISTRY_DIR}/hosts.toml"
[host."http://${reg_name}:5000"]
  capabilities = ["pull", "resolve"]
EOF
  # Proxy ARP helps gVisor networking on kind
  container exec "${node}" sysctl -w net.ipv4.conf.all.proxy_arp=1 >/dev/null || true
done

# 4. Attach registry to kind network
if [[ "$(container inspect -f '{{json .NetworkSettings.Networks}}' "${reg_name}")" != *'"kind"'* ]]; then
  log "Connecting registry to kind network"
  container network connect kind "${reg_name}" || true
fi

kubectl --context="${KUBECTL_CONTEXT}" apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "localhost:${reg_port}"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF

log "Cluster ready: context ${KUBECTL_CONTEXT}"
kubectl --context="${KUBECTL_CONTEXT}" get nodes
