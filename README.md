# MCP Tool Call Enforcement with Tetragon & Cilium

Defense-in-depth enforcement for Model Context Protocol (MCP) tool calls on Kubernetes—outside the LLM.

## Architecture (3 Layers)

```
LLM / Client ──► [1. Go MCP Gateway] ──► [2. Cilium Network Policy] ──► [3. Tetragon eBPF Runtime]
                  Human approval gate     L3/L4 egress lockdown          SIGKILL on shell execve
```

| Layer | Component | What It Blocks | Key File |
| :--- | :--- | :--- | :--- |
| **1. Application** | Go MCP Gateway | High-risk `tools/call` (`drop_database`, `modify_infra`, `exec_shell`) held in `PENDING_APPROVAL` until human sign-off | `mcp-enforcement/assets/gateway/main.go` |
| **2. Network** | Cilium (`CiliumNetworkPolicy`) | Unauthorized egress / data exfiltration from `mcp-tool-runner` (allows only `kube-dns` and `approved-backend:8080`) | `mcp-enforcement/assets/policies/cnp-mcp-egress.yaml` |
| **3. Kernel Runtime** | Tetragon (`TracingPolicyNamespaced`) | Shell execution (`/sh`, `/bash`, `/dash`, `/zsh`) via `sys_execve` hook with immediate `SIGKILL` | `mcp-enforcement/assets/policies/tp-block-shell.yaml` |

---

## Quick Start (Local Kind Cluster)

**Prerequisites:** `docker`, `kind`, `kubectl` (with Cilium & Tetragon installed in the cluster for Layers 2 & 3).

```bash
# 1. Create Kind cluster and deploy workloads
make up

# 2. Layer 1: Test low-risk pass-through vs. high-risk human approval gate
make demo-gateway

# 3. Layer 2: Apply Cilium egress lockdown and test blocked exfiltration
make apply-cilium

# 4. Layer 3: Apply Tetragon syscall policy and test shell SIGKILL (exit 137)
make apply-tetragon

# 5. View Tetragon enforcement events
make events

# 6. Clean up resources
make reset
```

---

## Manual Verification Commands

### Layer 1: Application Approval Gate
```bash
# Low-risk tool (succeeds immediately)
curl -sS -X POST http://localhost:30090/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_inventory"}}'

# High-risk tool (blocks until approved)
curl -sS -X POST http://localhost:30090/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"drop_database"}}' &

# List pending requests & approve
curl -sS http://localhost:30090/pending
curl -sS -X POST "http://localhost:30090/approve?id=<REQUEST_ID>"
```

### Layer 2: Cilium Egress Lockdown
```bash
kubectl apply -f mcp-enforcement/assets/policies/cnp-mcp-egress.yaml

# Blocked external call
kubectl -n mcp-demo exec deploy/mcp-tool-runner -- curl -sSI --max-time 5 https://example.com

# Allowed internal backend
kubectl -n mcp-demo exec deploy/mcp-tool-runner -- curl -sSI http://approved-backend.mcp-demo.svc.cluster.local:8080/
```

### Layer 3: Tetragon Kernel Enforcement
```bash
kubectl apply -f mcp-enforcement/assets/policies/tp-block-shell.yaml

# Killed by kernel with exit code 137 (SIGKILL)
kubectl exec -n mcp-demo deploy/mcp-tool-runner -- /bin/sh -c "id"
```

---

## Repository Structure

```text
.
├── Makefile                        # Local cluster & demo targets
├── structure.json                  # Killercoda scenario root config
├── scripts/push-ghcr.sh            # Build & push gateway image to GHCR
└── mcp-enforcement/                # Killercoda interactive lab scenario
    ├── intro.md / step1-3.md       # Walkthrough steps
    ├── background.sh               # Automated cluster & Cilium/Tetragon provisioning
    └── assets/
        ├── gateway/                # Go MCP approval proxy + K8s manifests
        ├── policies/               # Cilium & Tetragon YAML policies
        └── scripts/                # Automated test scripts per layer
```
