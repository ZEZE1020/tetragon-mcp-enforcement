#!/bin/bash
set -euo pipefail

GATEWAY_URL="${GATEWAY_URL:-http://localhost:30090/mcp}"
ADMIN_URL="${ADMIN_URL:-http://localhost:30090}"

echo "==> [1/5] MCP Handshake (initialize) & Tool Discovery (tools/list)"
curl -sS -X POST "$GATEWAY_URL" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}' | sed 's/^/INIT: /'
echo

echo "==> [2/5] Sending LOW-RISK tools/call (list_inventory) - forwarded immediately via mcp-tool-runner"
curl -sS -X POST "$GATEWAY_URL" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_inventory","arguments":{"scope":"demo"}}}' | sed 's/^/LOW-RISK RESPONSE: /'
echo -e "\n"

echo "==> [3/5] Sending HIGH-RISK tools/call (drop_database) - held in PENDING_APPROVAL"
echo "This request blocks until approved; running in background..."
curl -sS -X POST "$GATEWAY_URL" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"drop_database","arguments":{"target":"prod"}}}' > /tmp/high_risk_response.json &
HIGH_RISK_PID=$!

sleep 2

echo "==> [4/5] Querying /pending to show held request"
PENDING_JSON="$(curl -sS "${ADMIN_URL}/pending")"
echo "$PENDING_JSON"

# Extract latest PENDING_APPROVAL id without requiring jq
APPROVAL_ID="$(echo "$PENDING_JSON" | grep -o '"id":"[0-9]*","tool":"[^"]*","status":"PENDING_APPROVAL"' | tail -n1 | grep -o '"id":"[0-9]*"' | cut -d'"' -f4 || true)"
if [ -z "${APPROVAL_ID}" ]; then
  echo "ERROR: Could not find a PENDING_APPROVAL id. Gateway/admin endpoint may be unavailable."
  wait "$HIGH_RISK_PID" || true
  cat /tmp/high_risk_response.json || true
  exit 1
fi

echo -e "\n==> [5/5] Approving pending request id=${APPROVAL_ID}"
curl -sS -X POST "${ADMIN_URL}/approve?id=${APPROVAL_ID}" | sed 's/^/APPROVE RESPONSE: /'
echo

echo "Waiting for high-risk call to complete..."
wait "$HIGH_RISK_PID"
echo "HIGH-RISK RESPONSE: $(cat /tmp/high_risk_response.json)"
echo -e "\nDone. Low-risk call was immediate; high-risk call required human approval."
