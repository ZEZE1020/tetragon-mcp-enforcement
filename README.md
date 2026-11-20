# MCP Tool Call Enforcement with Tetragon, Cilium & Hubble

Defense-in-depth enforcement for Model Context Protocol (MCP) tool calls on Kubernetes—deterministically enforced **outside the LLM**, with **Native MCP Apps (`ui://`)** and live **Cilium Hubble UI** flow visibility.

## Architecture (3 Chained Layers)

```text
LLM / Web Console (:30090)
   │  MCP Streamable HTTP (JSON-RPC 2.0 + Native MCP Apps ui://)
   ▼
[Layer 1: mcp-gateway (:8090 / NodePort 30090)]   ← Human Approval Gate (PENDING_APPROVAL)
   │  Approved & Low-Risk Tool Calls
   ▼
[Layer 2: mcp-tool-runner (:8081) + Cilium CNP]   ← L3/L4 Egress Lockdown + Hubble UI (:31235)
   │  Allowed: kube-dns:53 & approved-backend:8080
   │  Blocked: External internet exfiltration (RED DROPPED in Hubble UI)
   ▼
[Layer 3: Tetragon eBPF TracingPolicyNamespaced]  ← Kernel sys_execve -> SIGKILL (exit 137)
```

| Layer | Component | What It Enforces | Key File |
| :--- | :--- | :--- | :--- |
| **1. Application** | Go MCP Gateway (`--mode=gateway`) | Full MCP server (`initialize`, `tools/list`, `resources/list`, `resources/read`, `tools/call`) + Native MCP Apps (`ui://`) + `PENDING_APPROVAL` gate for high-risk & unknown tools | `mcp-enforcement/assets/gateway/main.go` |
| **2. Network** | Cilium (`CiliumNetworkPolicy`) + **Hubble UI** | Egress allowlist for `mcp-tool-runner` (`kube-dns:53` & `approved-backend:8080` only); visualizes `FORWARDED` vs. `DROPPED` flows live | `mcp-enforcement/assets/policies/cnp-mcp-egress.yaml` |
| **3. Kernel Runtime** | Tetragon (`TracingPolicyNamespaced`) | Kills `/sh`, `/bash`, `/dash`, `/zsh` via eBPF `sys_execve` hook with immediate `SIGKILL` (`exit 137`) | `mcp-enforcement/assets/policies/tp-block-shell.yaml` |

---

## Endpoints, Ports & MCP Tools

| Service | Cluster Port | NodePort | Kind Local URL | Killercoda Link | Purpose |
| :--- | :---: | :---: | :--- | :--- | :--- |
| **`mcp-gateway`** | `8090` | `30090` | `http://localhost:30090/` | `{{TRAFFIC_HOST1_30090}}` | Web Workbench + Native MCP Apps `<iframe>` Host + Approval Queue |
| **`mcp-gateway` (MCP API)** | `8090` | `30090` | `http://localhost:30090/mcp` | `{{TRAFFIC_HOST1_30090}}/mcp` | Standard MCP Streamable HTTP JSON-RPC 2.0 endpoint (`/pending`, `/approve`, `/reject`, `/healthz`) |
| **`hubble-ui`** | `80` | `31235` | `http://localhost:31235/` | `{{TRAFFIC_HOST1_31235}}` | Live Cilium service map showing green `FORWARDED` and red `DROPPED` flows |
| **`mcp-tool-runner`** | `8081` | ClusterIP | Internal only | Internal only | Executes `fetch_url` (L2) and `exec_shell` (L3) inside sandboxed pod |
| **`approved-backend`** | `8080` | ClusterIP | Internal only | Internal only | Approved internal inventory/admin API |

### MCP Tools & Native MCP App UI Resources (`text/html;profile=mcp-app`)

| Tool | Risk Tier | Defense Layer Exercised | Native MCP UI Resource (`_meta.ui.resourceUri`) |
| :--- | :---: | :--- | :--- |
| `list_inventory` | `LOW` | L1 Pass → L2 Allowed (`runner` → `approved-backend`) | `ui://mcp-demo/inventory-view` |
| `get_status` | `LOW` | L1 Gateway posture & live counters | `ui://mcp-demo/security-posture` |
| `drop_database` | `HIGH` | L1 Human Approval Gate (`PENDING_APPROVAL`) | `ui://mcp-demo/security-posture` |
| `modify_infra` | `HIGH` | L1 Human Approval Gate (`PENDING_APPROVAL`) | `ui://mcp-demo/security-posture` |
| `fetch_url` | `HIGH` | L1 Approval → L2 Cilium Egress Allow/Drop + Hubble UI | `ui://mcp-demo/egress-result` |
| `exec_shell` | `HIGH` | L1 Approval → L3 Tetragon `sys_execve` `SIGKILL` (137) | `ui://mcp-demo/runtime-result` |

*(No external LLM API key is required—the built-in Web Console at `:30090` provides 1-click simulated agent scenarios, and any external MCP client such as Claude, VS Code, or MCP Inspector can connect directly to `/mcp`.)*

---

## Quick Start (Local 2-Node Kind Cluster)

**Prerequisites:** `docker`, `kind`, `kubectl`, `helm`.

```bash
# 1. Create 2-node Kind cluster (control-plane + worker, disableDefaultCNI),
#    install Cilium + Hubble UI (:31235) + Tetragon, build/load image & deploy mcp-demo
make up

# 2. Open the UIs in your browser:
#    • MCP Console & Native MCP Apps UI : http://localhost:30090
#    • Cilium Hubble UI (select mcp-demo): http://localhost:31235

# 3. Verify MCP protocol (initialize, tools/list, resources/list) & Layer 1 gate
make verify-mcp
make demo-gateway

# 4. Layer 2: Apply Cilium egress lockdown, test blocked exfiltration & check Hubble UI
make apply-cilium
make hubble-flows

# 5. Layer 3: Apply Tetragon syscall policy, test shell SIGKILL (exit 137) & view events
make apply-tetragon
make events

# 6. Reset workloads/policies (or delete cluster with `make down`)
make reset
```

### Publishing Updated Image for Killercoda

Killercoda pulls `ghcr.io/zeze1020/mcp-gateway:latest` (single ~15MB multi-mode image used by all 3 pods in `mcp-demo`, eliminating Docker Hub rate-limit stalls):

```bash
make docker-push
```

---

## Repository Structure

```text
.
├── kind-config.yaml                # 2-node Kind cluster (disableDefaultCNI + ports 30090, 31235)
├── Makefile                        # End-to-end Kind provisioning, Cilium/Hubble, Tetragon & demo targets
├── structure.json                  # Killercoda scenario root config
├── scripts/push-ghcr.sh            # Multi-arch build & push to GHCR
└── mcp-enforcement/                # Killercoda interactive lab (kubernetes-kubeadm-1node-4GB)
    ├── index.json                  # Scenario config & /root/mcp-lab asset layout
    ├── intro.md / step1-3.md       # Interactive guides with MCP Console & Hubble UI links
    ├── background.sh               # Dynamic CNI swap -> Cilium+Hubble, Tetragon, mcp-demo
    ├── foreground.sh               # Live stage progress & fail-fast diagnostics
    └── assets/
        ├── gateway/                # Multi-mode Go MCP server, Native MCP Apps UI, Dockerfile & k8s.yaml
        ├── policies/               # Cilium CNP & Tetragon TracingPolicyNamespaced YAMLs
        └── scripts/                # Layer 1, 2, 3 verification scripts
```
