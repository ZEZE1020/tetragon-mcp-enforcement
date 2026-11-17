#!/bin/bash
set -euo pipefail

NAMESPACE="${NAMESPACE:-mcp-demo}"
DEPLOY="${DEPLOY:-deploy/mcp-tool-runner}"

echo "==> Attempting shell exec inside ${DEPLOY} (should be SIGKILL'ed by Tetragon policy)"
set +e
kubectl -n "$NAMESPACE" exec "$DEPLOY" -- /bin/sh -c "id"
RC=$?
set -e

echo "kubectl exec exit code: $RC"
if [ "$RC" -eq 137 ]; then
  echo "Observed exit code 137 (SIGKILL) - runtime enforcement confirmed."
elif [ "$RC" -ne 0 ]; then
  echo "Process was blocked/killed (non-zero exit). Depending on environment, SIGKILL may surface as 137."
else
  echo "WARNING: Shell exec succeeded; ensure tp-block-shell.yaml is applied and Tetragon is running."
fi

echo
echo "==> Recent Tetragon events (compact) for namespace ${NAMESPACE}:"
kubectl -n kube-system exec ds/tetragon -c tetragon -- tetra getevents -o compact --namespace "$NAMESPACE" --since 5m | tail -n 30 || \
  echo "(tetra CLI not available via kubectl exec in this environment; rely on exit code 137 as enforcement signal)"

echo
echo "Shell escape enforcement check complete."