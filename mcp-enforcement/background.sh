#!/bin/bash
set -euo pipefail

LOG_FILE="/var/log/killercoda-setup.log"
STATUS_FILE="/tmp/setup-status"
DONE_FILE="/tmp/setup-complete"
FAIL_FILE="/tmp/setup-failed"
LAB_DIR="/root/mcp-lab"

exec > >(tee -a "$LOG_FILE") 2>&1

set_status() {
  echo "$1" > "$STATUS_FILE"
  echo "[background] $(date -Is) - $1"
}

on_error() {
  local rc=$?
  local stage
  stage="$(cat "$STATUS_FILE" 2>/dev/null || echo 'unknown')"
  echo "Setup failed during: ${stage} (exit ${rc})" > "$FAIL_FILE"
  echo "[background] ERROR during '${stage}' (exit ${rc}). Cluster snapshot:"
  kubectl get nodes -o wide 2>/dev/null || true
  kubectl get pods -A 2>/dev/null || true
  exit "$rc"
}
trap on_error ERR

export DEBIAN_FRONTEND=noninteractive
rm -f "$DONE_FILE" "$FAIL_FILE"

set_status "[1/5] Waiting for Kubernetes node & preparing /root/mcp-lab..."

for _ in $(seq 1 90); do
  if kubectl get nodes >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
kubectl get nodes >/dev/null 2>&1

# Ensure single-node control plane can schedule workloads
kubectl taint nodes --all node-role.kubernetes.io/control-plane- 2>/dev/null || true
kubectl taint nodes --all node-role.kubernetes.io/master- 2>/dev/null || true

# Fix script permissions (keep YAML/Go non-executable)
chmod +x "$LAB_DIR"/scripts/*.sh 2>/dev/null || true

# Reduce Killercoda Theia editor clutter by hiding root dotfiles/caches
mkdir -p /root/.theia "$LAB_DIR/.theia"
cat <<'EOF' > /root/.theia/settings.json
{
  "files.exclude": {
    "**/.git": true,
    "**/.cache": true,
    "**/.config": true,
    "**/.local": true,
    "**/.kube": true,
    "**/.helm": true,
    "**/.ssh": true,
    "**/.theia": true,
    "**/.bashrc": true,
    "**/.profile": true,
    "**/.bash_history": true,
    "**/.viminfo": true,
    "**/.wget-hsts": true,
    "**/snap": true
  }
}
EOF
cp /root/.theia/settings.json "$LAB_DIR/.theia/settings.json"

set_status "[2/5] Installing Helm & replacing default CNI with Cilium + Hubble UI..."

if ! command -v helm >/dev/null 2>&1; then
  curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
fi

# Detect and remove pre-installed Canal/Flannel/Calico so Cilium doesn't conflict or stall
for ds in canal kube-flannel-ds calico-node; do
  if kubectl -n kube-system get ds "$ds" >/dev/null 2>&1; then
    echo "[background] Removing pre-installed CNI DaemonSet: $ds"
    kubectl -n kube-system delete ds "$ds" --ignore-not-found=true
  fi
done
rm -f /etc/cni/net.d/*canal* /etc/cni/net.d/*flannel* /etc/cni/net.d/*calico* 2>/dev/null || true

helm repo add cilium https://helm.cilium.io >/dev/null
helm repo update >/dev/null

# Single-pass Helm install: Cilium + 1 operator replica + Hubble Relay + Hubble UI on NodePort 31235
# envoy.enabled=false saves ~400MB image pull & RAM (L3/L4 CNP + Hubble work without Envoy)
helm upgrade --install cilium cilium/cilium \
  --namespace kube-system \
  --set operator.replicas=1 \
  --set envoy.enabled=false \
  --set cni.exclusive=true \
  --set hubble.enabled=true \
  --set hubble.relay.enabled=true \
  --set hubble.ui.enabled=true \
  --set hubble.ui.service.type=NodePort \
  --set hubble.ui.service.nodePort=31235 \
  --wait \
  --timeout 5m

# Restart CoreDNS so it gets a Cilium-managed identity & endpoint cleanly
kubectl -n kube-system rollout restart deploy/coredns 2>/dev/null || true
kubectl -n kube-system rollout status deploy/coredns --timeout=120s 2>/dev/null || true
kubectl wait --for=condition=Ready node --all --timeout=120s

set_status "[3/5] Installing Tetragon eBPF runtime security..."

helm upgrade --install tetragon cilium/tetragon \
  --namespace kube-system \
  --wait \
  --timeout 4m

kubectl -n kube-system rollout status ds/tetragon --timeout=180s

set_status "[4/5] Deploying mcp-demo workloads (gateway, tool-runner, approved-backend)..."

kubectl apply -f "$LAB_DIR/gateway/k8s.yaml"

# Wait for all 3 deployments with fail-fast image-pull diagnostics
wait_deploy() {
  local name="$1"
  if ! kubectl -n mcp-demo rollout status "deploy/${name}" --timeout=180s; then
    echo "[background] Deployment ${name} failed to become ready. Pod diagnostics:"
    kubectl -n mcp-demo get pods -o wide || true
    kubectl -n mcp-demo describe pods -l "app=${name}" | tail -n 35 || true
    return 1
  fi
}

wait_deploy approved-backend
wait_deploy mcp-tool-runner
wait_deploy mcp-gateway

set_status "[5/5] Verifying MCP Gateway (:30090) & Hubble UI (:31235)..."

for _ in $(seq 1 30); do
  if curl -fsS http://localhost:30090/healthz >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
curl -fsS http://localhost:30090/healthz >/dev/null

set_status "Ready! MCP Gateway (:30090) + Hubble UI (:31235) + Tetragon are online."
touch "$DONE_FILE"
