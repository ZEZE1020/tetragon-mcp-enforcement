# Layer 2 — Cilium Network Enforcement

Even with an approval gateway, a compromised or confused tool-runner process might attempt data exfiltration or contact untrusted endpoints. Cilium enforces **identity-aware L3/L4 egress policy** in the kernel (eBPF) so the pod can only reach what it needs.

## 1. Inspect the policy

```bash
cat /root/mcp-lab/assets/policies/cnp-mcp-egress.yaml
```{{exec}}

This `CiliumNetworkPolicy` selects pods labeled `app: mcp-tool-runner` in namespace `mcp-demo` and allows egress **only** to:
- `kube-dns` (UDP/TCP 53) for name resolution
- `app: approved-backend` on TCP 8080

Everything else (including the public internet) is denied by default for the selected endpoints.

## 2. Test connectivity before applying the policy

From the tool-runner pod, both the approved backend and an external site should be reachable (baseline):

```bash
kubectl -n mcp-demo exec deploy/mcp-tool-runner -- sh -c 'curl -sS --max-time 5 -o /dev/null -w "approved-backend: %{http_code}\n" http://approved-backend.mcp-demo.svc.cluster.local:8080/ || echo "approved-backend: FAILED"'
```{{exec}}

```bash
kubectl -n mcp-demo exec deploy/mcp-tool-runner -- sh -c 'curl -sS --max-time 5 -o /dev/null -w "example.com: %{http_code}\n" https://example.com || echo "example.com: FAILED"'
```{{exec}}

## 3. Apply the Cilium policy and re-test

```bash
kubectl apply -f /root/mcp-lab/assets/policies/cnp-mcp-egress.yaml
```{{exec}}

```bash
/root/mcp-lab/assets/scripts/test-exfiltration.sh
```{{exec}}

## 4. Verify

- Requests to `approved-backend:8080` should **succeed**.
- Requests to `https://example.com` should **fail/timeout**, demonstrating blocked exfiltration.

> **Takeaway:** Network policy constrains blast radius by limiting where tool execution can communicate, independent of what the LLM requested. Next, we’ll stop shell escapes at runtime with Tetragon.