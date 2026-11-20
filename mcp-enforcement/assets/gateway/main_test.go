package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func setupTestStack(t *testing.T) (*httptest.Server, *httptest.Server, *httptest.Server) {
	t.Helper()
	backend := httptest.NewServer(newBackendMux())
	runner := httptest.NewServer(newRunnerMux())
	gw := NewGateway(backend.URL, runner.URL)
	gateway := httptest.NewServer(newGatewayMux(gw))
	t.Cleanup(func() {
		gateway.Close()
		runner.Close()
		backend.Close()
	})
	return gateway, runner, backend
}

func postJSON(t *testing.T, url string, payload any) map[string]any {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func TestMCPProtocolAndNativeUIs(t *testing.T) {
	gw, _, _ := setupTestStack(t)

	// 1. Console HTML served on /
	resp, err := http.Get(gw.URL + "/")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / failed: status=%v err=%v", resp, err)
	}
	resp.Body.Close()

	// 2. initialize
	initRes := postJSON(t, gw.URL+"/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2025-06-18"},
	})
	resObj, _ := initRes["result"].(map[string]any)
	if resObj["protocolVersion"] != "2025-06-18" {
		t.Fatalf("unexpected initialize result: %#v", initRes)
	}

	// 3. tools/list includes _meta.ui.resourceUri
	toolsRes := postJSON(t, gw.URL+"/mcp", map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	tools := toolsRes["result"].(map[string]any)["tools"].([]any)
	if len(tools) < 5 {
		t.Fatalf("expected >= 5 tools, got %d", len(tools))
	}

	// 4. resources/list and resources/read serve text/html;profile=mcp-app
	listRes := postJSON(t, gw.URL+"/mcp", map[string]any{"jsonrpc": "2.0", "id": 3, "method": "resources/list"})
	resources := listRes["result"].(map[string]any)["resources"].([]any)
	if len(resources) < 4 {
		t.Fatalf("expected >= 4 ui:// resources, got %d", len(resources))
	}

	readRes := postJSON(t, gw.URL+"/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 4, "method": "resources/read",
		"params": map[string]any{"uri": "ui://mcp-demo/inventory-view"},
	})
	contents := readRes["result"].(map[string]any)["contents"].([]any)
	first := contents[0].(map[string]any)
	if first["mimeType"] != "text/html;profile=mcp-app" || !strings.Contains(first["text"].(string), "ui/notifications/tool-result") {
		t.Fatalf("unexpected native MCP app resource: %#v", first)
	}
}

func TestLowRiskAndHighRiskApprovalFlow(t *testing.T) {
	gw, _, backend := setupTestStack(t)

	// Low-risk list_inventory executes immediately via runner -> backend
	lowRes := postJSON(t, gw.URL+"/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 10, "method": "tools/call",
		"params": map[string]any{"name": "list_inventory", "arguments": map[string]any{"scope": "demo"}},
	})
	sc := lowRes["result"].(map[string]any)["structuredContent"].(map[string]any)
	if sc["via_runner"] != true {
		t.Fatalf("expected list_inventory to route via runner, got %#v", sc)
	}

	// High-risk fetch_url blocks until approved, then executes via runner
	done := make(chan map[string]any, 1)
	go func() {
		done <- postJSON(t, gw.URL+"/mcp", map[string]any{
			"jsonrpc": "2.0", "id": 11, "method": "tools/call",
			"params": map[string]any{"name": "fetch_url", "arguments": map[string]any{"url": backend.URL + "/inventory"}},
		})
	}()

	time.Sleep(150 * time.Millisecond)

	// Verify /pending has id=1
	resp, err := http.Get(gw.URL + "/pending")
	if err != nil {
		t.Fatalf("GET /pending: %v", err)
	}
	var pending struct {
		Pending []PendingCall `json:"pending"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&pending)
	resp.Body.Close()
	if len(pending.Pending) != 1 || pending.Pending[0].Status != StatusPending {
		t.Fatalf("expected 1 pending call, got %#v", pending.Pending)
	}

	// Approve it
	apResp, err := http.Post(gw.URL+"/approve?id="+pending.Pending[0].ID, "application/json", nil)
	if err != nil || apResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /approve failed: %v %v", apResp, err)
	}
	apResp.Body.Close()

	select {
	case highRes := <-done:
		res := highRes["result"].(map[string]any)["structuredContent"].(map[string]any)
		if res["allowed"] != true {
			t.Fatalf("expected approved fetch_url to succeed, got %#v", res)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for approved high-risk call")
	}

	// High-risk exec_shell rejected returns -32001 error
	doneRej := make(chan map[string]any, 1)
	go func() {
		doneRej <- postJSON(t, gw.URL+"/mcp", map[string]any{
			"jsonrpc": "2.0", "id": 12, "method": "tools/call",
			"params": map[string]any{"name": "exec_shell", "arguments": map[string]any{"command": "id"}},
		})
	}()

	time.Sleep(150 * time.Millisecond)
	rejResp, err := http.Post(gw.URL+"/reject?id=2", "application/json", nil)
	if err != nil || rejResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /reject failed: %v %v", rejResp, err)
	}
	rejResp.Body.Close()

	select {
	case rejRes := <-doneRej:
		errObj, ok := rejRes["error"].(map[string]any)
		if !ok || int(errObj["code"].(float64)) != -32001 {
			t.Fatalf("expected -32001 rejection error, got %#v", rejRes)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for rejected call")
	}
}
