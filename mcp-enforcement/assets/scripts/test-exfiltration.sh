#!/bin/bash
set -euo pipefail

NAMESPACE="${NAMESPACE:-mcp-demo}"
DEPLOY="${DEPLOY:-deploy/mcp-tool-runner}"

echo "==> Testing egress from ${DEPLOY} in namespace ${NAMESPACE}"
echo "    (Invoking curl directly without /bin/sh so Layer 3 shell-block does not interfere)"
echo

echo "[1/2] Attempting access to approved-backend:8080 (should SUCCEED — FORWARDED in Hubble UI)..."
if kubectl -n "$NAMESPACE" exec "$DEPLOY" -- curl -sS --max-time 5 -o /dev/null -w "HTTP %{http_code}\n" http://approved-backend.mcp-demo.svc.cluster.local:8080/inventory; then
  echo "approved-backend: SUCCESS (ALLOWED)"
else
  echo "approved-backend: FAILED (unexpected if policy allows backend)"
fi

echo
echo "[2/2] Attempting exfiltration to https://example.com (should be BLOCKED — DROPPED in Hubble UI)..."
set +e
kubectl -n "$NAMESPACE" exec "$DEPLOY" -- curl -sS --max-time 5 -o /dev/null https://example.com
RC=$?
set -e
if [ "$RC" -eq 0 ]; then
  echo "example.com: SUCCESS (unexpected if Cilium egress policy is enforced)"
  exit 1
else
  echo "example.com: BLOCKED/TIMEOUT as expected (curl exit code $RC)"
fi

echo
echo "Egress enforcement check complete. Inspect live FORWARDED/DROPPED flows in Hubble UI (:31235)."
