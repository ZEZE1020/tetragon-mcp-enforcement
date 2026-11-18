#!/bin/bash
set -euo pipefail

GATEWAY_URL="${GATEWAY_URL:-http://localhost:30090/mcp}"
ADMIN_URL="${ADMIN_URL:-http://localhost:30090}"

echo "==> [1/4] Sending LOW-RISK tools/call (list_inventory) - expected to forward immediately"
curl -sS -X POST "$GATEWAY_URL" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_inventory","arguments":{"scope":"demo"}}}' | sed 's/^/LOW-RISK RESPONSE: /'
echo -e "\n"

echo "==> [2/4] Sending HIGH-RISK tools/call (drop_database) - expected to hold in PENDING_APPROVAL"
echo "This request will block until approved; running it in background..."
curl -sS -X POST "$GATEWAY_URL" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"drop_database","arguments":{"target":"prod"}}}' > /tmp/high_risk_response.json &
HIGH_RISK_PID=$!

sleep 2

echo "==> [3/4] Querying /pending to show held request"
PENDING_JSON="$(curl -sS "${ADMIN_URL}/pending")"
echo "$PENDING_JSON"

# Extract first pending id (simple grep/sed to avoid jq dependency)
APPROVAL_ID="$(echo "$PENDING_JSON" | grep -o '"id":"[0-9]*"' | head -n1 | cut -d'"' -f4 || true)"
if [ -z "${APPROVAL_ID}" ]; then
  echo "ERROR: Could not find a pending approval id. Gateway/admin endpoint may be unavailable."
  echo "Waiting briefly for background request to finish for diagnostics..."
  wait "$HIGH_RISK_PID" || true
  cat /tmp/high_risk_response.json || true
  exit 1
fi

echo -e "\n==> [4/4] Approving pending request id=${APPROVAL_ID}"
curl -sS -X POST "${ADMIN_URL}/approve?id=${APPROVAL_ID}" | sed 's/^/APPROVE RESPONSE: /'
echo

echo "Waiting for high-risk call to complete..."
wait "$HIGH_RISK_PID"
echo "HIGH-RISK RESPONSE: $(cat /tmp/high_risk_response.json)"
echo -e "\nDone. Low-risk call was immediate; high-risk call required approval."