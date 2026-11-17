# MCP Tool Call Enforcement with Tetragon & Cilium

Model Context Protocol (MCP) lets LLM agents invoke tools via structured JSON-RPC `tools/call` requests. But when an agent’s context is manipulated (prompt injection, poisoned documents, confused-deputy flows), **the tool call itself is often perfectly valid JSON-RPC**. Prompt filtering and output guardrails can’t reliably stop a well-formed, authorized-looking request that the model was tricked into making.

This lab demonstrates **defense in depth** with three enforcement layers that operate outside the LLM:

1. **Layer 1 — MCP Approval Gateway (Go):** A JSON-RPC proxy that classifies tool calls by risk. Low-risk read tools are forwarded immediately; high-risk tools (e.g. `drop_database`, `modify_infra`, `exec_shell`) are held in `PENDING_APPROVAL` until explicitly approved.
2. **Layer 2 — Cilium Network Enforcement:** A `CiliumNetworkPolicy` restricts the tool-runner pod’s egress to only DNS and the approved backend, blocking exfiltration to the public internet.
3. **Layer 3 — Tetragon Runtime Enforcement:** A `TracingPolicyNamespaced` hooks `sys_execve` and sends `SIGKILL` on shell execution attempts (`/bin/sh`, `/bin/bash`, etc.) inside the tool-runner, stopping runtime escapes even if a call gets through.

By the end, you’ll see how to combine application-layer approval, identity-aware network policy, and kernel-level runtime enforcement to contain manipulated MCP tool calls.