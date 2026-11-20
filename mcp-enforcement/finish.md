# Wrap-Up & Takeaways

You validated a complete, 3-layer defense-in-depth architecture for MCP tool-call enforcement—enforced deterministically **outside the LLM**:

1. **Layer 1 — Protocol / Intent (`mcp-gateway` on `:30090`):** Full MCP Streamable HTTP server with Native MCP Apps (`ui://`) that holds high-risk and unknown `tools/call` requests in `PENDING_APPROVAL` until explicit human sign-off.
2. **Layer 2 — Network Egress (`Cilium` + `Hubble UI` on `:31235`):** Identity-aware L3/L4 egress lockdown limiting `mcp-tool-runner` strictly to `kube-dns` and `approved-backend:8080`, with real-time `FORWARDED`/`DROPPED` visibility in Hubble UI.
3. **Layer 3 — Kernel Syscalls (`Tetragon`):** eBPF `TracingPolicyNamespaced` hooking `sys_execve` to immediately `SIGKILL` (`exit 137`) unauthorized shell execution inside the tool runner.

## Adapting for Production MCP Deployments

- **Roll out safely:** Start Tetragon policies in audit/observability mode (`Post`) to baseline normal tool behavior before enabling `Sigkill`/`Override`.
- **Use distroless where possible:** Pair Tetragon process ancestry rules with immutable, non-root, read-only root filesystems.
- **Dynamic risk policies:** Evolve static tool risk tiers into argument-aware policies (OPA/Cedar) wired to Slack/PagerDuty approval workflows.
- **Run locally on Kind:** Clone the repository and run `make up` to spin up a 2-node Kind cluster with Cilium, Hubble UI (`http://localhost:31235`), Tetragon, and the MCP Gateway (`http://localhost:30090`).

## Resources

- **GitHub repository:** [https://github.com/ZEZE1020/tetragon-mcp-enforcement](https://github.com/ZEZE1020/tetragon-mcp-enforcement)
- **Speaker (MCP Dev Summit Nairobi):** ZEZE1020 — [https://github.com/ZEZE1020](https://github.com/ZEZE1020)
