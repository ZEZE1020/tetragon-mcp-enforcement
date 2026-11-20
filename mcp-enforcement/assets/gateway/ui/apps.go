package ui

import (
	_ "embed"
)

//go:embed console.html
var ConsoleHTML []byte

// AppResource describes a native MCP App UI resource served via resources/list and resources/read.
type AppResource struct {
	URI         string
	Name        string
	Title       string
	Description string
	MimeType    string
	HTML        string
}

const MCPAppMimeType = "text/html;profile=mcp-app"

// Resources returns all native MCP App UI resources exposed by the MCP gateway.
func Resources() []AppResource {
	return []AppResource{
		{
			URI:         "ui://mcp-demo/inventory-view",
			Name:        "inventory-view",
			Title:       "Cluster Inventory (MCP App)",
			Description: "Interactive native MCP App view rendering approved backend inventory records.",
			MimeType:    MCPAppMimeType,
			HTML:        inventoryAppHTML,
		},
		{
			URI:         "ui://mcp-demo/security-posture",
			Name:        "security-posture",
			Title:       "3-Layer Defense Posture (MCP App)",
			Description: "Interactive native MCP App view showing Gateway, Cilium, and Tetragon enforcement posture.",
			MimeType:    MCPAppMimeType,
			HTML:        postureAppHTML,
		},
		{
			URI:         "ui://mcp-demo/egress-result",
			Name:        "egress-result",
			Title:       "Network Egress Inspection (MCP App)",
			Description: "Native MCP App view visualizing Layer 2 Cilium egress allow/drop outcomes from mcp-tool-runner.",
			MimeType:    MCPAppMimeType,
			HTML:        egressAppHTML,
		},
		{
			URI:         "ui://mcp-demo/runtime-result",
			Name:        "runtime-result",
			Title:       "Kernel Runtime Syscall Telemetry (MCP App)",
			Description: "Native MCP App view showing Layer 3 Tetragon sys_execve execution and SIGKILL (137) enforcement.",
			MimeType:    MCPAppMimeType,
			HTML:        runtimeAppHTML,
		},
	}
}

// LookupResource finds a native MCP App UI resource by URI.
func LookupResource(uri string) (AppResource, bool) {
	for _, r := range Resources() {
		if r.URI == uri {
			return r, true
		}
	}
	return AppResource{}, false
}

const sharedAppStyle = `<style>
  :root { color-scheme: dark; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
  * { box-sizing: border-box; }
  body { margin: 0; padding: 14px; background: #0d1117; color: #e6edf3; font-size: 13px; }
  .card { background: #161b22; border: 1px solid #30363d; border-radius: 10px; padding: 14px; }
  .hdr { display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px; gap: 8px; }
  .title { font-weight: 700; font-size: 14px; display: flex; align-items: center; gap: 8px; }
  .badge { font-size: 11px; font-weight: 700; padding: 3px 8px; border-radius: 999px; letter-spacing: 0.3px; }
  .ok { background: #122d1f; color: #3fb950; border: 1px solid #238636; }
  .warn { background: #2d2208; color: #d29922; border: 1px solid #9e6a03; }
  .err { background: #331418; color: #f85149; border: 1px solid #da3633; }
  .info { background: #12233b; color: #58a6ff; border: 1px solid #1f6feb; }
  table { width: 100%; border-collapse: collapse; margin-top: 8px; }
  th, td { text-align: left; padding: 7px 8px; border-bottom: 1px solid #21262d; }
  th { color: #8b949e; font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; }
  .mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; }
  .grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; margin-top: 10px; }
  .metric { background: #0d1117; border: 1px solid #21262d; border-radius: 8px; padding: 10px; }
  .metric-label { color: #8b949e; font-size: 11px; text-transform: uppercase; }
  .metric-val { font-size: 18px; font-weight: 700; margin-top: 4px; }
  pre { background: #0d1117; border: 1px solid #21262d; border-radius: 8px; padding: 10px; overflow-x: auto; margin: 8px 0 0; white-space: pre-wrap; word-break: break-word; }
  .btn { background: #21262d; color: #e6edf3; border: 1px solid #30363d; border-radius: 6px; padding: 5px 10px; font-size: 12px; cursor: pointer; }
  .btn:hover { background: #30363d; }
  .muted { color: #8b949e; font-size: 12px; }
</style>`

const mcpAppBridgeScript = `<script>
  // Minimal MCP Apps host <-> sandboxed iframe JSON-RPC postMessage bridge
  function sendToHost(method, params) {
    window.parent.postMessage({ jsonrpc: "2.0", method: method, params: params || {} }, "*");
  }
  window.addEventListener("message", function (ev) {
    const msg = ev.data;
    if (!msg || typeof msg !== "object") return;
    if (msg.method === "ui/notifications/tool-result" && typeof window.onToolResult === "function") {
      window.onToolResult(msg.params || {});
    }
  });
  sendToHost("ui/notifications/initialized", { ready: true });
</script>`

const inventoryAppHTML = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Inventory MCP App</title>` + sharedAppStyle + `</head>
<body>
  <div class="card">
    <div class="hdr">
      <div class="title"><span>📦 Production Service Inventory</span><span class="badge ok">LOW RISK · AUTO-ALLOWED</span></div>
      <button class="btn" onclick="sendToHost('tools/call', {name:'list_inventory', arguments:{scope:'demo'}})">↻ Refresh via MCP</button>
    </div>
    <div id="meta" class="muted">Waiting for tool result via MCP Apps postMessage bridge...</div>
    <table>
      <thead><tr><th>Service</th><th>Namespace</th><th>Tier</th><th>Status</th><th>Endpoint</th></tr></thead>
      <tbody id="rows"><tr><td colspan="5" class="muted">No records loaded yet.</td></tr></tbody>
    </table>
  </div>
  ` + mcpAppBridgeScript + `
  <script>
    window.onToolResult = function(payload) {
      const data = payload.structuredContent || payload;
      const items = (data && data.items) || [];
      document.getElementById("meta").textContent =
        "Source: " + (data.source || "approved-backend") + " | Scope: " + (data.scope || "demo") + " | Records: " + items.length;
      const tbody = document.getElementById("rows");
      if (!items.length) {
        tbody.innerHTML = '<tr><td colspan="5" class="muted">Empty inventory response</td></tr>';
        return;
      }
      tbody.innerHTML = items.map(function(it) {
        return '<tr>' +
          '<td class="mono">' + escapeHtml(it.name || "-") + '</td>' +
          '<td class="mono">' + escapeHtml(it.namespace || "mcp-demo") + '</td>' +
          '<td><span class="badge info">' + escapeHtml(it.tier || "app") + '</span></td>' +
          '<td><span class="badge ok">' + escapeHtml(it.status || "healthy") + '</span></td>' +
          '<td class="mono">' + escapeHtml(it.endpoint || "-") + '</td>' +
          '</tr>';
      }).join("");
    };
    function escapeHtml(s) {
      return String(s).replace(/[&<>"']/g, function(c) {
        return {"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c];
      });
    }
  </script>
</body>
</html>`

const postureAppHTML = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Security Posture MCP App</title>` + sharedAppStyle + `</head>
<body>
  <div class="card">
    <div class="hdr">
      <div class="title"><span>🛡️ 3-Layer MCP Enforcement Posture</span><span id="postureBadge" class="badge info">LIVE</span></div>
      <button class="btn" onclick="sendToHost('tools/call', {name:'get_status', arguments:{}})">↻ Refresh</button>
    </div>
    <div class="grid">
      <div class="metric"><div class="metric-label">Layer 1 · Gateway</div><div id="m1" class="metric-val" style="color:#3fb950">ACTIVE</div><div id="m1sub" class="muted">Approval Gate</div></div>
      <div class="metric"><div class="metric-label">Layer 2 · Cilium</div><div id="m2" class="metric-val" style="color:#58a6ff">EGRESS L3/L4</div><div class="muted">Hubble Observable</div></div>
      <div class="metric"><div class="metric-label">Layer 3 · Tetragon</div><div id="m3" class="metric-val" style="color:#d29922">eBPF SYSCALL</div><div class="muted">sys_execve → SIGKILL</div></div>
    </div>
    <pre id="raw" class="mono">Waiting for tool result...</pre>
  </div>
  ` + mcpAppBridgeScript + `
  <script>
    window.onToolResult = function(payload) {
      const data = payload.structuredContent || payload;
      if (data && data.counts) {
        document.getElementById("m1sub").textContent =
          "Pending: " + data.counts.pending + " | Approved: " + data.counts.approved + " | Rejected: " + data.counts.rejected;
      }
      if (data && data.action === "drop_database") {
        document.getElementById("postureBadge").className = "badge warn";
        document.getElementById("postureBadge").textContent = "APPROVED HIGH-RISK ACTION";
      }
      document.getElementById("raw").textContent = JSON.stringify(data, null, 2);
    };
  </script>
</body>
</html>`

const egressAppHTML = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Cilium Egress MCP App</title>` + sharedAppStyle + `</head>
<body>
  <div class="card">
    <div class="hdr">
      <div class="title"><span>🌐 Layer 2 · Network Egress Result (Cilium)</span><span id="statusBadge" class="badge info">CHECKING</span></div>
    </div>
    <div id="summary" class="muted">Awaiting mcp-tool-runner egress response...</div>
    <pre id="details" class="mono"></pre>
  </div>
  ` + mcpAppBridgeScript + `
  <script>
    window.onToolResult = function(payload) {
      const data = payload.structuredContent || payload;
      const badge = document.getElementById("statusBadge");
      const allowed = !!data.allowed;
      badge.className = "badge " + (allowed ? "ok" : "err");
      badge.textContent = allowed ? "FORWARDED (ALLOWED)" : "BLOCKED BY CILIUM / TIMEOUT";
      document.getElementById("summary").textContent =
        "Target URL: " + (data.url || "-") + " | Latency: " + (data.duration_ms || 0) + "ms | Open Hubble UI (:31235) to inspect flow verdict.";
      document.getElementById("details").textContent = JSON.stringify(data, null, 2);
    };
  </script>
</body>
</html>`

const runtimeAppHTML = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Tetragon Runtime MCP App</title>` + sharedAppStyle + `</head>
<body>
  <div class="card">
    <div class="hdr">
      <div class="title"><span>⚡ Layer 3 · Kernel Runtime Result (Tetragon)</span><span id="statusBadge" class="badge info">CHECKING</span></div>
    </div>
    <div id="summary" class="muted">Awaiting mcp-tool-runner syscall execution...</div>
    <pre id="details" class="mono"></pre>
  </div>
  ` + mcpAppBridgeScript + `
  <script>
    window.onToolResult = function(payload) {
      const data = payload.structuredContent || payload;
      const badge = document.getElementById("statusBadge");
      if (data.killed_by_sigkill || data.exit_code === 137) {
        badge.className = "badge err";
        badge.textContent = "SIGKILL · EXIT 137 (TETRAGON)";
      } else if (data.exit_code === 0) {
        badge.className = "badge warn";
        badge.textContent = "SHELL EXECUTED (POLICY NOT APPLIED)";
      } else {
        badge.className = "badge err";
        badge.textContent = "EXIT " + (data.exit_code ?? "?");
      }
      document.getElementById("summary").textContent =
        "Command: /bin/sh -c " + JSON.stringify(data.command || "") + " | Pod: " + (data.pod || "mcp-tool-runner");
      document.getElementById("details").textContent = JSON.stringify(data, null, 2);
    };
  </script>
</body>
</html>`
