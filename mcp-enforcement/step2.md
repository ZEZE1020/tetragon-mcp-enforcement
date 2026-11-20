# Layer 2: Network Egress Lockdown (Cilium) & Hubble UI

Even if a tool call is approved at Layer 1 (e.g., `fetch_url`), a compromised tool runner must not be able to exfiltrate data to arbitrary internet endpoints. **Cilium** enforces identity-aware L3/L4 egress in the eBPF datapath, and **Hubble UI** lets you watch every `FORWARDED` and `DROPPED` flow live.

## 1. Open Hubble UI

Open **[Hubble UI (:31235)]({{TRAFFIC_HOST1_31235}})** in a new tab and select the **`mcp-demo`** namespace in the top-left dropdown.

## 2. Baseline (before policy): unhardened external egress

Test reaching `https://example.com` directly from `deploy/mcp-tool-runner` (invoking `curl` directly without `/bin/sh`):

```bash
kubectl -n mcp-demo exec deploy/mcp-tool-runner -- curl -sSI --max-time 5 https://example.com | head -n 1
```{{exec}}

Expected: `HTTP/2 200` (baseline external egress is allowed—and visible as a green `FORWARDED` flow to `world` in Hubble UI).

## 3. Inspect and apply the Cilium egress policy

Review the policy (allows only DNS to `kube-dns:53` and TCP `8080` to `app=approved-backend` in `mcp-demo`):

```bash
cat /root/mcp-lab/policies/cnp-mcp-egress.yaml
```{{exec}}

Apply the `CiliumNetworkPolicy`:

```bash
kubectl apply -f /root/mcp-lab/policies/cnp-mcp-egress.yaml
kubectl -n mcp-demo get ciliumnetworkpolicies.cilium.io
```{{exec}}

## 4. Watch Cilium block exfiltration in real time (CLI + Hubble UI + MCP Console)

Run the combined exfiltration check:

```bash
/root/mcp-lab/scripts/test-exfiltration.sh
```{{exec}}

Switch to **[Hubble UI (:31235)]({{TRAFFIC_HOST1_31235}})** (namespace `mcp-demo`) and observe:
- **Green (`FORWARDED`):** `mcp-tool-runner` → `approved-backend:8080` and `kube-dns:53`
- **Red (`DROPPED — Policy denied`):** `mcp-tool-runner` → `world` (`example.com:443`)!

> **Try it end-to-end through the MCP Console!** Open **[MCP Console (:30090)]({{TRAFFIC_HOST1_30090}})**, click **Scenario 5 (`fetch_url` → `https://example.com`)**, click **Approve** at Layer 1, and watch Layer 2 (Cilium) STILL block the exfiltration and render the red **BLOCKED BY CILIUM** native MCP App card!

**Takeaway:** Least-privilege Cilium egress policies stop data exfiltration in the kernel datapath—even if Layer 1 approval was granted.
