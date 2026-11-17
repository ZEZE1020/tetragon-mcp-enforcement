# Layer 1 — MCP Approval Gateway

The Go-based MCP gateway proxies JSON-RPC 2.0 `tools/call` requests. Low-risk read tools are forwarded immediately to the approved backend; high-risk tools are held in a `PENDING_APPROVAL` state until a human approves or rejects them.

## 1. Inspect the gateway and workloads

Confirm the lab namespace and pods are running:

```bash
kubectl -n mcp-demo get pods,svc
```{{exec}}

View the gateway code (risk classification and approval state machine):

```bash
sed -n '1,220p' /root/mcp-lab/assets/gateway/main.go
```{{exec}}

## 2. Run the demo: low-risk vs high-risk tool calls

The helper script sends a low-risk `list_inventory` call (forwarded immediately) and a high-risk `drop_database` call (held for approval), then shows the pending queue and approves it.

```bash
/root/mcp-lab/assets/scripts/trigger-high-risk.sh
```{{exec}}

## 3. Verify behavior

- The **low-risk** call returns immediately with a proxied response from `approved-backend`.
- The **high-risk** call blocks until approved; after approval the gateway forwards it and returns the backend response.
- You can manually inspect pending requests at any time:

```bash
curl -s http://localhost:30090/pending | jq . 2>/dev/null || curl -s http://localhost:30090/pending
```{{exec}}

> **Takeaway:** Even if an LLM is manipulated into emitting a valid `tools/call`, the gateway enforces an out-of-band approval boundary for dangerous operations. Next, we’ll constrain where the tool runner can talk on the network with Cilium.