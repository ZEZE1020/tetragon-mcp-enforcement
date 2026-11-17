# Layer 3: Kernel Syscall Enforcement (Tetragon)

Kernel-level enforcement is the final backstop. If an MCP tool has a command-injection vulnerability or attempts to spawn a reverse shell, network and application controls alone cannot stop local process execution inside the container. Tetragon uses eBPF to observe and enforce syscall behavior in the Linux kernel.

## 1. Baseline (before policy): shell execution succeeds

Before runtime policy enforcement, spawning a shell in the tool-runner pod succeeds:

```bash
kubectl exec -n mcp-demo deploy/mcp-tool-runner -- /bin/sh -c "id"
```{{exec}}

Expected: command runs successfully and prints the container user identity.

## 2. Inspect and apply the Tetragon runtime policy

Inspect the namespaced policy that hooks `sys_execve` for common shells and kills the process with `SIGKILL`:

```bash
cat /root/mcp-lab/assets/policies/tp-block-shell.yaml
```{{exec}}

This `TracingPolicyNamespaced` selects `app: mcp-tool-runner` in `mcp-demo` and matches `sys_execve` arguments ending in `/sh`, `/bash`, `/dash`, `/zsh` with `action: Sigkill`.

Apply it:

```bash
kubectl apply -f /root/mcp-lab/assets/policies/tp-block-shell.yaml
```{{exec}}

```bash
kubectl -n mcp-demo get tracingpoliciesnamespaced.cilium.io
```{{exec}}

Give Tetragon a moment to load the hook, then continue.

## 3. Re-test: shell execution is killed by the kernel

Re-run the same shell command. It should be terminated before `/bin/sh` fully initializes:

```bash
kubectl exec -n mcp-demo deploy/mcp-tool-runner -- /bin/sh -c "id" || true
```{{exec}}

```bash
kubectl exec -n mcp-demo deploy/mcp-tool-runner -- /bin/sh -c "id"; echo "exit_code=$?"
```{{exec}}

Expected: **exit code `137`** (`SIGKILL` = `128 + 9`), showing kernel-enforced termination.

> **Scripted check:** `/root/mcp-lab/assets/scripts/test-shell-escape.sh` also attempts the exec and prints recent Tetragon events:
> ```bash
> /root/mcp-lab/assets/scripts/test-shell-escape.sh
> ```{{exec}}

## 4. Observe Tetragon kernel telemetry

Tail recent enforcement events from Tetragon's agent (`tetra` CLI) for the `mcp-demo` namespace:

```bash
kubectl exec -n kube-system ds/tetragon -c tetragon -- tetra getevents -o compact --namespace mcp-demo | tail -n 10
```{{exec}}

Expected: events showing the blocked `execve` and enforcement outcome (e.g. `SIGKILL` / `💥 exit` entries) for shell binaries in the tool-runner pod.

**Takeaway:** even with application approval and network controls in place, Tetragon provides a kernel-level last line of defense against in-container process execution abuse.