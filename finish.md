# Finish — Defense in Depth for MCP Tool Calls

You’ve implemented and validated three complementary enforcement layers for MCP tool execution:

1. **Gateway approval (application layer):** A Go JSON-RPC proxy forwards low-risk `tools/call` requests but holds high-risk actions for explicit approval, preventing silent execution of dangerous operations produced by manipulated LLM context.
2. **Cilium egress policy (network layer):** The tool-runner pod can resolve DNS and talk only to the approved backend, blocking exfiltration paths to untrusted destinations.
3. **Tetragon TracingPolicy (runtime layer):** `sys_execve` attempts to spawn shells inside the tool-runner are terminated with `SIGKILL`, stopping post-exploitation/runtime escape techniques.

## Key lesson
Prompt filtering alone is insufficient because the attack often arrives as **valid, well-formed MCP JSON-RPC**. Robust protection comes from enforcing least privilege and explicit authorization **outside** the model: at the tool boundary, on the network, and in the kernel.

## Clean up (optional)

```bash
kubectl delete -f /root/mcp-lab/assets/policies/tp-block-shell.yaml --ignore-not-found
kubectl delete -f /root/mcp-lab/assets/policies/cnp-mcp-egress.yaml --ignore-not-found
kubectl delete -f /root/mcp-lab/assets/gateway/k8s.yaml --ignore-not-found
```{{exec}}

Thanks for completing the lab!