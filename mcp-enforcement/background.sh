#!/bin/bash
set -euo pipefail

exec > >(tee -a /var/log/killercoda-setup.log) 2>&1

echo "[background] Starting setup at $(date -Is)"

export DEBIAN_FRONTEND=noninteractive
LAB_DIR="/root/mcp-lab"

wait_for_k8s() {
  echo "[background] Waiting for Kubernetes node to be Ready..."
  for i in $(seq 1 300); do
    if kubectl get nodes >/dev/null 2>&1; then
      if kubectl wait --for=condition=Ready node --all --timeout=10s >/dev/null 2>&1; then
        echo "[background] Kubernetes is Ready"
        return 0
      fi
    fi
    sleep 2
  done
  echo "[background] Timed out waiting for Kubernetes"
  exit 1
}

wait_for_k8s

# Install Helm if missing
if ! command -v helm >/dev/null 2>&1; then
  echo "[background] Installing Helm..."
  curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
fi

# Install Cilium CLI
echo "[background] Installing Cilium CLI..."
CILIUM_CLI_VERSION="$(curl -fsSL https://raw.githubusercontent.com/cilium/cilium-cli/main/stable.txt)"
CLI_ARCH=amd64
if [ "$(uname -m)" = "aarch64" ]; then CLI_ARCH=arm64; fi
curl -fsSL -L --remote-name-all "https://github.com/cilium/cilium-cli/releases/download/${CILIUM_CLI_VERSION}/cilium-linux-${CLI_ARCH}.tar.gz{,.sha256sum}"
sha256sum --check "cilium-linux-${CLI_ARCH}.sha256sum"
tar xzvf "cilium-linux-${CLI_ARCH}.tar.gz" -C /usr/local/bin
rm "cilium-linux-${CLI_ARCH}.tar.gz" "cilium-linux-${CLI_ARCH}.sha256sum"

# Install Cilium CNI
echo "[background] Installing Cilium..."
cilium install --wait --wait-duration 5m

echo "[background] Cilium status:"
cilium status --wait || true

# Install Tetragon
echo "[background] Installing Tetragon..."
helm repo add cilium https://helm.cilium.io >/dev/null
helm repo update >/dev/null
helm upgrade --install tetragon cilium/tetragon -n kube-system --wait --timeout 5m

kubectl -n kube-system rollout status ds/tetragon --timeout=300s

# Deploy lab workloads
echo "[background] Deploying lab workloads (pulling published images)..."
kubectl apply -f "$LAB_DIR/assets/gateway/k8s.yaml"

echo "[background] Waiting for pods in mcp-demo namespace to be Ready..."
kubectl -n mcp-demo rollout status deploy/approved-backend --timeout=300s
kubectl -n mcp-demo rollout status deploy/mcp-gateway --timeout=300s
kubectl -n mcp-demo rollout status deploy/mcp-tool-runner --timeout=300s
kubectl -n mcp-demo wait --for=condition=Ready pod --all --timeout=300s

echo "[background] Setup complete at $(date -Is)"
touch /tmp/setup-complete
