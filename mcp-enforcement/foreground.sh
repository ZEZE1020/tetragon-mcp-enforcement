#!/bin/bash
set -euo pipefail

STATUS_FILE="/tmp/setup-status"
DONE_FILE="/tmp/setup-complete"
FAIL_FILE="/tmp/setup-failed"
LOG_FILE="/var/log/killercoda-setup.log"

echo "🚀 Provisioning MCP Tool Call Enforcement lab (Cilium + Hubble UI + Tetragon + mcp-demo)..."
echo "   Tip: live progress updates appear below (typically 2-3 minutes)."
echo ""

LAST_STATUS=""
while [ ! -f "$DONE_FILE" ] && [ ! -f "$FAIL_FILE" ]; do
  if [ -f "$STATUS_FILE" ]; then
    CUR_STATUS="$(cat "$STATUS_FILE" 2>/dev/null || true)"
    if [ -n "$CUR_STATUS" ] && [ "$CUR_STATUS" != "$LAST_STATUS" ]; then
      echo "⏳ $CUR_STATUS"
      LAST_STATUS="$CUR_STATUS"
    fi
  fi
  sleep 2
done

echo ""
if [ -f "$FAIL_FILE" ]; then
  echo "❌ $(cat "$FAIL_FILE")"
  echo "--- Last 25 lines of $LOG_FILE ---"
  tail -n 25 "$LOG_FILE" 2>/dev/null || true
  echo "----------------------------------"
  cd /root/mcp-lab 2>/dev/null || cd /root
  exec bash
fi

echo "✅ Environment is ready!"
echo ""
echo "   • Lab Workspace : /root/mcp-lab"
echo "   • MCP Console   : NodePort 30090 (Native MCP Apps UI + Approval Queue + /mcp)"
echo "   • Hubble UI     : NodePort 31235 (Live Cilium network flows & policy drops)"
echo ""
kubectl get pods -n mcp-demo
echo ""
cd /root/mcp-lab
exec bash
