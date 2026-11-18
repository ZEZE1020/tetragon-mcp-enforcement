#!/bin/bash
set -euo pipefail

echo "🚀 Preparing the MCP Tool Call Enforcement lab environment..."
echo "This may take a few minutes (installing Cilium & Tetragon, building/deploying workloads)."

while [ ! -f /tmp/setup-complete ]; do
  printf "."
  sleep 2
done

echo ""
echo "✅ Environment is ready!"
echo ""
cd /root/mcp-lab
exec bash
