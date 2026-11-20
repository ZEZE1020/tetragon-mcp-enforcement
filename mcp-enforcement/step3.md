# Layer 3: Kernel Syscall Enforcement (Tetragon)

Kernel-level enforcement is the final backstop. If an MCP tool has a command-injection bug or an approved tool attempts to spawn a shell inside the container, **Tetragon** intercepts `sys_execve` in the Linux kernel via eBPF and immediately kills the process with **`SIGKILL`** (`exit 137`).

## 1. Baseline (before policy): shell execution succeeds

Before applying the Tetragon policy, spawning `/bin/sh` inside `mcp-tool-runner` succeeds:

```bash
kubectl exec -n mcp-demo deploy/mcp-tool-runner -- /bin/sh -c "id"
```{{exec}}

Expected: prints `uid=1000(mcp) gid=1000(mcp)` (shell execution works).

## 2. Inspect and apply the Tetragon runtime policy

Inspect `tp-block-shell.yaml` (`TracingPolicyNamespaced` selecting `app: mcp-tool-runner`, hooking `sys_execve` for `/sh`, `/bash`, `/dash`, `/zsh` with `action: Sigkill`):

```bash
cat /root/mcp-lab/policies/tp-block-shell.yaml
```{{exec}}

Apply it:

```bash
kubectl apply -f /root/mcp-lab/policies/tp-block-shell.yaml
kubectl -n mcp-demo get tracingpoliciesnamespaced.cilium.io
```{{exec}}

## 3. Re-test: shell execution is killed by the kernel (`exit 137`)

Run the shell escape verification script (triggers `/bin/sh -c "id"` and streams matching `tetra getevents` from the Tetragon pod on `mcp-tool-runner`'s node):

```bash
/root/mcp-lab/scripts/test-shell-escape.sh
```{{exec}}

Expected:
- **`exit code 137`** (`128 + 9` = `SIGKILL`) — the kernel terminated `/bin/sh` before `id` could execute.
- Tetragon's `tetra getevents` shows the exact `mcp-tool-runner` pod, binary `/bin/sh`, and **`SIGKILL`** enforcement event.

> **Try it end-to-end through the MCP Console!** Open **[MCP Console (:30090)]({{TRAFFIC_HOST1_30090}})**, click **Scenario 6 (`exec_shell` → `id`)**, click **Approve** at Layer 1, and watch Tetragon kill the spawned `/bin/sh` process in the kernel while `mcp-tool-runner` stays alive and renders the **SIGKILL · EXIT 137 (TETRAGON)** native MCP App card!

**Takeaway:** Even if Layer 1 is bypassed or approved, Tetragon enforces immutable process boundaries in the Linux kernel.
