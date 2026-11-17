# Layer 3 — Tetragon Runtime Syscall Enforcement

Network controls don’t stop a process from spawning a shell inside the pod. Tetragon uses eBPF + kprobes/tracepoints to observe and **enforce** runtime behavior at the kernel level. Here we kill any `execve` of common shells inside the tool-runner.

## 1. Inspect the TracingPolicy

```bash
cat /root/mcp-lab/assets/policies/tp-block-shell.yaml
```{{exec}}

This `TracingPolicyNamespaced` (scoped to `mcp-demo`, selecting `app: mcp-tool-runner`) hooks `sys_execve` and matches binaries ending in `/sh`, `/bash`, `/dash`, or `/zsh`, issuing a `Sigkill` action.

## 2. Apply the policy

```bash
kubectl apply -f /root/mcp-lab/assets/policies/tp-block-shell.yaml
```{{exec}}

Give Tetragon a moment to load the policy, then confirm the resource exists:

```bash
kubectl -n mcp-demo get tracingpoliciesnamespaced.cilium.io
```{{exec}}

## 3. Attempt a shell escape

Try to exec a shell in the tool-runner pod (this should be killed):

```bash
/root/mcp-lab/assets/scripts/test-shell-escape.sh
```{{exec}}

You should see a non-zero exit (typically **137**, indicating `SIGKILL`).

## 4. Observe enforcement events

If the `tetra` CLI is available in-cluster, you can view compact events:

```bash
kubectl -n kube-system exec ds/tetragon -c tetragon -- tetra getevents -o compact --namespace mcp-demo --since 5m | tail -n 20 || echo "tetra CLI not available in this environment; policy enforcement (exit 137) still confirms Sigkill."
```{{exec}}

> **Takeaway:** Kernel-level enforcement stops dangerous runtime actions (like spawning shells) even when application and network layers are bypassed or misconfigured. Together with the gateway and Cilium, this forms a layered defense for MCP tool execution.