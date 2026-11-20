# Layer 1: MCP Approval Gateway & Native MCP Apps UI

The Go MCP gateway (`mcp-gateway`) implements MCP Streamable HTTP (`POST /mcp`), exposes **Native MCP App UI resources** (`ui://mcp-demo/*`, MIME `text/html;profile=mcp-app`), and gates every `tools/call` by risk tier before forwarding execution to `mcp-tool-runner` or `approved-backend`.

## 1. Open the MCP Enforcement Console (Native MCP Apps UI)

You can interact visually in the browser **without needing an LLM API key**:

- **[Open MCP Enforcement Console (:30090)]({{TRAFFIC_HOST1_30090}})** — Simulated Agent Scenarios, Human Approval Queue, Sandboxed Native MCP App `<iframe>` renderer, and JSON-RPC 2.0 Wire Inspector.
- **[Open Cilium Hubble UI (:31235)]({{TRAFFIC_HOST1_31235}})** — Live service map & traffic flows (we'll use this heavily in Step 2).

Verify the `mcp-demo` workloads in the terminal:

```bash
kubectl get pods -n mcp-demo
```{{exec}}

## 2. Inspect MCP Protocol Handshake, Tools & Native `ui://` Resources

Query `tools/list` to see the 6 MCP tools and their `_meta.ui.resourceUri` bindings:

```bash
curl -sS -X POST http://localhost:30090/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | head -c 1200; echo ""
```{{exec}}

List the native MCP App UI resources served by the gateway (`resources/list`):

```bash
curl -sS -X POST http://localhost:30090/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"resources/list"}'
```{{exec}}

## 3. Test Low-Risk vs. High-Risk Tool Calls

### a) Low-risk call (`list_inventory`) — immediate pass-through

```bash
curl -sS -X POST http://localhost:30090/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_inventory","arguments":{"scope":"demo"}}}'
```{{exec}}

Notice `"via_runner": true`: `mcp-gateway` routed the read through `mcp-tool-runner` → `approved-backend:8080`.

### b) High-risk call (`drop_database`) — held in `PENDING_APPROVAL`

Simulate an indirect prompt injection triggering `drop_database` (run in background so you can approve it either via the Web Console or CLI):

```bash
curl -sS -X POST http://localhost:30090/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"drop_database","arguments":{"target":"prod"}}}' > /tmp/high_risk_response.json &
echo "High-risk request PID: $!"
sleep 2
```{{exec}}

### c) Inspect `/pending` and approve (or click **Approve** in the Web Console!)

```bash
curl -sS http://localhost:30090/pending
```{{exec}}

Approve the latest pending call via CLI (if you haven't already clicked **Approve** in the Web Console):

```bash
APPROVAL_ID=$(curl -sS http://localhost:30090/pending | grep -o '"id":"[0-9]*","tool":"[^"]*","status":"PENDING_APPROVAL"' | tail -n1 | grep -o '"id":"[0-9]*"' | cut -d'"' -f4)
if [ -n "$APPROVAL_ID" ]; then
  echo "Approving request_id=${APPROVAL_ID}"
  curl -sS -X POST "http://localhost:30090/approve?id=${APPROVAL_ID}"
  echo ""
fi
wait
cat /tmp/high_risk_response.json
```{{exec}}

> **One-command demo script:**
> ```bash
> /root/mcp-lab/scripts/trigger-high-risk.sh
> ```{{exec}}

**Takeaway:** Even when an attacker tricks a model into emitting a valid high-risk MCP `tools/call`, Layer 1 enforces an out-of-band human approval boundary—and renders native `ui://` MCP App views directly in the host console.
