#!/bin/bash
set -euo pipefail

NAMESPACE="${NAMESPACE:-mcp-demo}"
DEPLOY="${DEPLOY:-deploy/mcp-tool-runner}"

echo "==> Testing egress from ${DEPLOY} in namespace ${NAMESPACE}"
echo

echo "[1/2] Attempting access to approved backend (should SUCCEED with policy applied)..."
if kubectl -n "$NAMESPACE" exec "$DEPLOY" -- sh -c 'curl -sS --max-time 5 -o /dev/null -w "HTTP %{http_code}\n" http://approved-backend.mcp-demo.svc.cluster.local:8080/'; then
  echo "approved-backend: SUCCESS"
else
  echo "approved-backend: FAILED (unexpected if policy allows backend)"
fi

echo
echo "[2/2] Attempting access to external internet https://example.com (should be BLOCKED/timeout with policy applied)..."
set +e
kubectl -n "$NAMESPACE" exec "$DEPLOY" -- sh -c 'curl -sS --max-time 5 https://example.com >/tmp/exfil.out 2>/tmp/exfil.err'
RC=$?
set -e
if [ $RC -eq 0 ]; then
  echo "example.com: SUCCESS (unexpected if Cilium egress policy is enforced)"
  exit 1
else
  echo "example.com: BLOCKED/FAILED as expected (exit code $RC)"
fi

echo
echo "Egress enforcement check complete."