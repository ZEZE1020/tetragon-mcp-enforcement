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
  echo "Process was blocked/killed (non-zero exit $RC). Depending on environment, SIGKILL may surface as 137."
else
  echo "WARNING: Shell exec succeeded; ensure tp-block-shell.yaml is applied and Tetragon is running."
fi

echo
echo "==> Recent Tetragon events (compact) for namespace ${NAMESPACE}:"
# Target the Tetragon pod on the exact node running mcp-tool-runner (works on 1-node & multi-node clusters)
RUNNER_NODE="$(kubectl -n "$NAMESPACE" get pod -l app=mcp-tool-runner -o jsonpath='{.items[0].spec.nodeName}' 2>/dev/null || true)"
TETRA_POD=""
if [ -n "$RUNNER_NODE" ]; then
  TETRA_POD="$(kubectl -n kube-system get pod -l app.kubernetes.io/name=tetragon --field-selector "spec.nodeName=${RUNNER_NODE}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
fi

if [ -n "$TETRA_POD" ]; then
  kubectl -n kube-system exec "$TETRA_POD" -c tetragon -- tetra getevents -o compact --namespace "$NAMESPACE" --since 5m | tail -n 30 || true
else
  kubectl -n kube-system exec ds/tetragon -c tetragon -- tetra getevents -o compact --namespace "$NAMESPACE" --since 5m | tail -n 30 || \
    echo "(tetra CLI not available via kubectl exec in this environment; rely on exit code 137 as enforcement signal)"
fi

echo
echo "Shell escape enforcement check complete."
