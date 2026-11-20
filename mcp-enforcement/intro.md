# MCP Tool Call Enforcement with Tetragon, Cilium & Hubble

Prompt guardrails inspect natural language, not execution. Once an LLM's context is poisoned via indirect prompt injection, it emits a syntactically valid MCP JSON-RPC `tools/call` payload that bypasses prompt filters entirely. This lab demonstrates **deterministic defense-in-depth outside the model**—no external LLM API key required.

## 3-Layer Architecture

1. **Layer 1 — Application Gate (`mcp-gateway`, NodePort `30090`):** A real MCP Streamable HTTP server (`initialize`, `tools/list`, `resources/list`, `resources/read`, `tools/call`) with **Native MCP Apps (`ui://`)** and a human-in-the-loop approval gate that holds high-risk calls in `PENDING_APPROVAL`.
2. **Layer 2 — Network Egress (`Cilium` + `Hubble UI`, NodePort `31235`):** Enforces identity-aware L3/L4 egress (`CiliumNetworkPolicy`) on `mcp-tool-runner` and visualizes live `FORWARDED` vs. `DROPPED` flows in **Hubble UI**.
3. **Layer 3 — Kernel Runtime (`Tetragon`):** Enforces eBPF `TracingPolicyNamespaced` syscall hooks that immediately `SIGKILL` (`exit 137`) any shell spawned inside `mcp-tool-runner`.

## Lab Environment

`background.sh` dynamically replaces the default CNI with **Cilium + Hubble UI**, installs **Tetragon**, and deploys the **`mcp-demo`** workloads into **`/root/mcp-lab`**. Wait for the live stage updates in the terminal to report **Ready**, then open **Step 1**.
