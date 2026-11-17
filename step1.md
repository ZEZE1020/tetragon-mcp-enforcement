# Layer 1: Application Approval Gate (Go)

The Go MCP gateway proxies JSON-RPC 2.0 `tools/call` requests. Low-risk tools are forwarded immediately; high-risk tools are classified and held in a `PENDING_APPROVAL` state until an explicit human decision (`APPROVED` / `REJECTED`) is recorded.

## 1. Verify the cluster and `mcp-demo` workloads

Confirm Kubernetes, Cilium/Tetragon dependencies, and the demo pods are running:

```bash
kubectl get pods -n mcp-demo
```{{exec}}

(You should see `approved-backend`, `mcp-gateway`, and `mcp-tool-runner` pods in `Running/Ready` state.)

## 2. Inspect the gateway approval state machine

The gateway treats read-style tools as low risk and dangerous tools as high risk. High-risk calls are queued and block until a decision is made.

```bash
sed -n '30,90p' /root/mcp-lab/assets/gateway/main.go
```{{exec}}

```bash
sed -n '138,180p' /root/mcp-lab/assets/gateway/main.go
```{{exec}}

Key logic (from `assets/gateway/main.go`):

```go
const (
	StatusPending  ApprovalStatus = "PENDING_APPROVAL"
	StatusApproved ApprovalStatus = "APPROVED"
	StatusRejected ApprovalStatus = "REJECTED"
)

// low-risk: list_inventory, get_status -> forwarded immediately
// high-risk: drop_database, modify_infra, exec_shell -> held for approval
// unknown tools are treated as high-risk by default (fail-safe)
```{{copy}}

## 3. Test JSON-RPC tool calls through the gateway

The gateway is exposed via NodePort `30090` in this lab (`http://localhost:30090/mcp`).

### a) Low-risk call (`list_inventory`) — succeeds immediately

```bash
curl -sS -X POST http://localhost:30090/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_inventory","arguments":{"scope":"demo"}}}'
```{{exec}}

Expected: immediate `200 OK` JSON-RPC response proxied/wrapped from the approved backend.

### b) High-risk call (`drop_database`) — intercepted and held

This request blocks until a human approves or rejects it. Run it in the background so you can inspect the pending queue:

```bash
curl -sS -X POST http://localhost:30090/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"drop_database","arguments":{"target":"prod"}}}' > /tmp/high_risk_response.json &
echo "High-risk request PID: $!"
sleep 2
```{{exec}}

Expected: the call does **not** complete yet; the gateway holds it with status `PENDING_APPROVAL` and assigns an approval `request_id` (`id`).

### c) Inspect pending calls, approve, and verify execution proceeds

List pending requests:

```bash
curl -sS http://localhost:30090/pending
```{{exec}}

Approve the held request (captures the first pending `id` and posts to `/approve?id=<id>`):

```bash
APPROVAL_ID=$(curl -sS http://localhost:30090/pending | grep -o '"id":"[0-9]*"' | head -n1 | cut -d'"' -f4)
echo "Approving request_id=${APPROVAL_ID}"
curl -sS -X POST "http://localhost:30090/approve?id=${APPROVAL_ID}"
wait
cat /tmp/high_risk_response.json
```{{exec}}

Expected: the pending call transitions to `APPROVED`, the originally blocked `tools/call` is forwarded to the backend, and the response is returned only **after** human approval.

> **One-command demo alternative:** you can run the full scripted flow (low-risk + high-risk + pending + approve) at any time:
> ```bash
> /root/mcp-lab/assets/scripts/trigger-high-risk.sh
> ```{{exec}}

**Takeaway:** even if an attacker induces a syntactically valid high-risk MCP `tools/call`, the application-layer gate enforces an out-of-band human approval boundary before execution.