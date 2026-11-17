# Layer 2: Network Egress Lockdown (Cilium)

Even if a tool call is approved (or an agent has legitimate execution rights), a compromised tool runner must not be able to exfiltrate data or reach arbitrary external endpoints. Cilium enforces identity-aware **L3/L4 egress policy** in the eBPF datapath so the pod can only talk to explicitly allowed destinations.

## 1. Baseline (before policy): unhardened egress

From `deploy/mcp-tool-runner` in `mcp-demo`, test an external destination. Before applying the Cilium policy, this should succeed:

```bash
kubectl -n mcp-demo exec deploy/mcp-tool-runner -- sh -c 'curl -sSI --max-time 5 https://example.com | head -n 1'
```{{exec}}

Expected: an HTTP status line such as `HTTP/2 200` (baseline external egress is reachable).

## 2. Inspect and apply the Cilium egress policy

Review the policy (allows DNS to `kube-dns` and TCP/8080 only to `app=approved-backend` in `mcp-demo`):

```bash
cat /root/mcp-lab/assets/policies/cnp-mcp-egress.yaml
```{{exec}}

Apply it:

```bash
kubectl apply -f /root/mcp-lab/assets/policies/cnp-mcp-egress.yaml
```{{exec}}

```bash
kubectl -n mcp-demo get ciliumnetworkpolicies.cilium.io
```{{exec}}

## 3. Re-test egress (after policy): blocked internet, allowed backend

Attempt to reach the public internet again (should be dropped/timed out by Cilium):

```bash
kubectl -n mcp-demo exec deploy/mcp-tool-runner -- sh -c 'curl -sSI --max-time 5 https://example.com | head -n 1 || echo "example.com: BLOCKED/TIMEOUT"'
```{{exec}}

Immediately test the approved in-cluster backend (should still succeed):

```bash
kubectl -n mcp-demo exec deploy/mcp-tool-runner -- sh -c 'curl -sS --max-time 5 -o /dev/null -w "approved-backend: HTTP %{http_code}\n" http://approved-backend.mcp-demo.svc.cluster.local:8080/'
```{{exec}}

> **Scripted check:** you can run the combined enforcement test script as well:
> ```bash
> /root/mcp-lab/assets/scripts/test-exfiltration.sh
> ```{{exec}}

**Takeaway:** least-privilege network isolation limits blast radius and blocks exfiltration paths, even when a tool invocation itself is valid or approved. Next, we enforce runtime behavior at the kernel level with Tetragon.