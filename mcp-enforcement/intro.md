# MCP Tool Call Enforcement with Tetragon & Cilium

Prompt guardrails inspect natural language, not execution. Once an LLM's context is poisoned via indirect prompt injection, it emits a syntactically valid MCP JSON-RPC `tools/call` payload that bypasses prompt filters entirely. The request looks correct to the protocol layer, so "safe" text-based controls cannot reliably stop a dangerous tool invocation that the model was tricked into making. This lab shows how to enforce MCP tool-call safety **outside the model** with defense in depth.

## What you will test (3-layer architecture)

1. **Application Layer (Go MCP Gateway):** Holds high-risk JSON-RPC calls in `PENDING_APPROVAL` until human sign-off, while allowing low-risk reads to proceed.
2. **Network Layer (Cilium):** Enforces L3/L4 egress policies (`CiliumNetworkPolicy`) so tool runners cannot exfiltrate data or reach unauthorized endpoints.
3. **Kernel Runtime Layer (Tetragon):** Enforces eBPF `TracingPolicyNamespaced` syscall hooks that `SIGKILL` compromised containers the instant they attempt `sys_execve` on a shell.

## Lab environment

`background.sh` automatically provisions **Cilium**, **Tetragon**, and the **`mcp-demo`** namespace workloads into **`/root/mcp-lab`**. Wait for the foreground setup check to complete, then continue to **Step 1** to verify the cluster and begin the demo.