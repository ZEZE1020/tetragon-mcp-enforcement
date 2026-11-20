package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"mcp-gateway/ui"
)

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      any             `json:"id,omitempty"`
}

type ToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type ResourceReadParams struct {
	URI string `json:"uri"`
}

type ApprovalStatus string

const (
	StatusPending  ApprovalStatus = "PENDING_APPROVAL"
	StatusApproved ApprovalStatus = "APPROVED"
	StatusRejected ApprovalStatus = "REJECTED"
)

type PendingCall struct {
	ID         string          `json:"id"`
	Tool       string          `json:"tool"`
	Status     ApprovalStatus  `json:"status"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
	Request    JSONRPCRequest  `json:"request"`
	RawRequest json.RawMessage `json:"-"`
	decision   chan ApprovalStatus
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
	Meta        map[string]any `json:"_meta,omitempty"`
}

type Gateway struct {
	backendURL string
	runnerURL  string
	client     *http.Client

	mu      sync.RWMutex
	pending map[string]*PendingCall
	counter uint64
}

func NewGateway(backendURL, runnerURL string) *Gateway {
	return &Gateway{
		backendURL: strings.TrimRight(backendURL, "/"),
		runnerURL:  strings.TrimRight(runnerURL, "/"),
		client:     &http.Client{Timeout: 15 * time.Second},
		pending:    make(map[string]*PendingCall),
	}
}

func (g *Gateway) nextID() string {
	n := atomic.AddUint64(&g.counter, 1)
	return strconv.FormatUint(n, 10)
}

func isLowRisk(tool string) bool {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "list_inventory", "get_status":
		return true
	default:
		return false
	}
}

func isHighRisk(tool string) bool {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "drop_database", "modify_infra", "exec_shell", "fetch_url":
		return true
	default:
		return false
	}
}

func toolCatalog() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_inventory",
			Title:       "List Service Inventory",
			Description: "Read-only query to approved-backend via mcp-tool-runner. Low-risk: forwarded immediately.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope": map[string]any{"type": "string", "description": "Environment scope (demo, prod)", "default": "demo"},
				},
			},
			Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false},
			Meta: map[string]any{
				"riskLevel": "LOW",
				"layer":     "L1 Pass → L2 Allowed",
				"ui":        map[string]any{"resourceUri": "ui://mcp-demo/inventory-view"},
			},
		},
		{
			Name:        "get_status",
			Title:       "Get Enforcement Posture",
			Description: "Returns gateway approval counters and active 3-layer defense posture. Low-risk: forwarded immediately.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false},
			Meta: map[string]any{
				"riskLevel": "LOW",
				"layer":     "L1 Gateway",
				"ui":        map[string]any{"resourceUri": "ui://mcp-demo/security-posture"},
			},
		},
		{
			Name:        "drop_database",
			Title:       "Drop Database",
			Description: "Destructive database operation. High-risk: held in PENDING_APPROVAL until human sign-off.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{"type": "string", "description": "Target database name"},
					"reason": map[string]any{"type": "string", "description": "Audit reason"},
				},
				"required": []string{"target"},
			},
			Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true},
			Meta: map[string]any{
				"riskLevel": "HIGH",
				"layer":     "L1 Approval Gate",
				"ui":        map[string]any{"resourceUri": "ui://mcp-demo/security-posture"},
			},
		},
		{
			Name:        "fetch_url",
			Title:       "Fetch URL from Tool Runner",
			Description: "Performs outbound HTTP fetch from mcp-tool-runner. High-risk: requires L1 approval, then subject to L2 Cilium egress policy (visible in Hubble UI).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{"type": "string", "description": "Destination URL to fetch"},
				},
				"required": []string{"url"},
			},
			Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false},
			Meta: map[string]any{
				"riskLevel": "HIGH",
				"layer":     "L1 Gate → L2 Cilium",
				"ui":        map[string]any{"resourceUri": "ui://mcp-demo/egress-result"},
			},
		},
		{
			Name:        "exec_shell",
			Title:       "Execute Shell Command",
			Description: "Spawns /bin/sh inside mcp-tool-runner. High-risk: requires L1 approval, then subject to L3 Tetragon sys_execve SIGKILL (exit 137).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "Shell command to attempt", "default": "id"},
				},
				"required": []string{"command"},
			},
			Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true},
			Meta: map[string]any{
				"riskLevel": "HIGH",
				"layer":     "L1 Gate → L3 Tetragon",
				"ui":        map[string]any{"resourceUri": "ui://mcp-demo/runtime-result"},
			},
		},
		{
			Name:        "modify_infra",
			Title:       "Modify Infrastructure",
			Description: "Mutates cluster/cloud infrastructure state. High-risk: held in PENDING_APPROVAL until human sign-off.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"resource": map[string]any{"type": "string", "description": "Infrastructure resource identifier"},
					"action":   map[string]any{"type": "string", "description": "Mutation action"},
				},
			},
			Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true},
			Meta: map[string]any{
				"riskLevel": "HIGH",
				"layer":     "L1 Approval Gate",
				"ui":        map[string]any{"resourceUri": "ui://mcp-demo/security-posture"},
			},
		},
	}
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept, Mcp-Session-Id, MCP-Protocol-Version")
	w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id")
}

func (g *Gateway) handleConsole(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(ui.ConsoleHTML)
}

func (g *Gateway) handleMCP(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSONRPCError(w, nil, -32700, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONRPCError(w, nil, -32700, "parse error")
		return
	}

	switch req.Method {
	case "initialize":
		g.handleInitialize(w, req)
		return
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
		return
	case "ping":
		writeJSONRPCResult(w, req.ID, map[string]any{})
		return
	case "tools/list":
		writeJSONRPCResult(w, req.ID, map[string]any{"tools": toolCatalog()})
		return
	case "resources/list":
		res := ui.Resources()
		out := make([]map[string]any, 0, len(res))
		for _, item := range res {
			out = append(out, map[string]any{
				"uri":         item.URI,
				"name":        item.Name,
				"title":       item.Title,
				"description": item.Description,
				"mimeType":    item.MimeType,
			})
		}
		writeJSONRPCResult(w, req.ID, map[string]any{"resources": out})
		return
	case "resources/read":
		var p ResourceReadParams
		if err := json.Unmarshal(req.Params, &p); err != nil || strings.TrimSpace(p.URI) == "" {
			writeJSONRPCError(w, req.ID, -32602, "invalid params: resources/read requires params.uri")
			return
		}
		res, ok := ui.LookupResource(p.URI)
		if !ok {
			writeJSONRPCError(w, req.ID, -32004, "unknown resource uri: "+p.URI)
			return
		}
		writeJSONRPCResult(w, req.ID, map[string]any{
			"contents": []map[string]any{
				{
					"uri":      res.URI,
					"mimeType": res.MimeType,
					"text":     res.HTML,
				},
			},
		})
		return
	case "tools/call":
		g.handleToolCall(w, r, req, body)
		return
	default:
		writeJSONRPCError(w, req.ID, -32601, "method not found: "+req.Method)
		return
	}
}

func (g *Gateway) handleInitialize(w http.ResponseWriter, req JSONRPCRequest) {
	proto := "2025-06-18"
	var initParams struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(req.Params) > 0 && json.Unmarshal(req.Params, &initParams) == nil && initParams.ProtocolVersion != "" {
		proto = initParams.ProtocolVersion
	}
	w.Header().Set("Mcp-Session-Id", fmt.Sprintf("mcp-demo-%d", time.Now().UnixNano()))
	writeJSONRPCResult(w, req.ID, map[string]any{
		"protocolVersion": proto,
		"capabilities": map[string]any{
			"tools":     map[string]any{"listChanged": false},
			"resources": map[string]any{"subscribe": false, "listChanged": false},
			"experimental": map[string]any{
				"io.modelcontextprotocol/ui": map[string]any{
					"mimeTypes": []string{ui.MCPAppMimeType},
				},
			},
		},
		"serverInfo": map[string]any{
			"name":    "mcp-enforcement-gateway",
			"version": "2.0.0",
		},
	})
}

func (g *Gateway) handleToolCall(w http.ResponseWriter, r *http.Request, req JSONRPCRequest, body []byte) {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil || strings.TrimSpace(params.Name) == "" {
		writeJSONRPCError(w, req.ID, -32602, "invalid params: tools/call requires params.name")
		return
	}

	tool := params.Name
	log.Printf("[gateway] tools/call name=%s id=%v", tool, req.ID)

	switch {
	case isLowRisk(tool):
		g.executeTool(w, req.ID, params, body)
		return
	case isHighRisk(tool):
		g.handleHighRisk(w, r, req, params, body)
		return
	default:
		// Fail-safe: unknown tools always require explicit human approval
		log.Printf("[gateway] unknown tool %q treated as high-risk", tool)
		g.handleHighRisk(w, r, req, params, body)
		return
	}
}

func (g *Gateway) handleHighRisk(w http.ResponseWriter, r *http.Request, req JSONRPCRequest, params ToolCallParams, raw []byte) {
	approvalID := g.nextID()
	pc := &PendingCall{
		ID:         approvalID,
		Tool:       params.Name,
		Status:     StatusPending,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		Request:    req,
		RawRequest: raw,
		decision:   make(chan ApprovalStatus, 1),
	}

	g.mu.Lock()
	g.pending[approvalID] = pc
	g.mu.Unlock()

	log.Printf("[gateway] high-risk call held: approval_id=%s tool=%s", approvalID, params.Name)

	select {
	case decision := <-pc.decision:
		switch decision {
		case StatusApproved:
			log.Printf("[gateway] approval_id=%s approved; executing tool=%s", approvalID, params.Name)
			g.executeTool(w, req.ID, params, raw)
		case StatusRejected:
			log.Printf("[gateway] approval_id=%s rejected", approvalID)
			writeJSONRPCError(w, req.ID, -32001, fmt.Sprintf("tool call rejected by approver (approval_id=%s)", approvalID))
		default:
			writeJSONRPCError(w, req.ID, -32002, "unknown approval decision")
		}
	case <-r.Context().Done():
		g.mu.Lock()
		if pc.Status == StatusPending {
			pc.Status = StatusRejected
			pc.UpdatedAt = time.Now().UTC()
		}
		g.mu.Unlock()
		log.Printf("[gateway] client disconnected while waiting: approval_id=%s", approvalID)
	}
}

func (g *Gateway) executeTool(w http.ResponseWriter, id any, params ToolCallParams, rawBody []byte) {
	name := strings.ToLower(strings.TrimSpace(params.Name))
	switch name {
	case "list_inventory":
		scope, _ := params.Arguments["scope"].(string)
		if scope == "" {
			scope = "demo"
		}
		targetURL := g.backendURL + "/inventory?scope=" + scope
		var structured map[string]any
		if g.runnerURL != "" {
			// Route through mcp-tool-runner so Hubble UI shows mcp-tool-runner -> approved-backend flow!
			fetchRes, err := g.callRunnerFetch(targetURL)
			if err == nil && fetchRes.Allowed && json.Valid([]byte(fetchRes.Body)) {
				_ = json.Unmarshal([]byte(fetchRes.Body), &structured)
				if structured != nil {
					structured["via_runner"] = true
					structured["source"] = "mcp-tool-runner → approved-backend"
				}
			}
		}
		if structured == nil {
			// Fallback to direct backend query
			resp, err := g.client.Get(targetURL)
			if err != nil {
				writeJSONRPCError(w, id, -32011, "approved-backend unreachable: "+err.Error())
				return
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			if json.Valid(b) {
				_ = json.Unmarshal(b, &structured)
			} else {
				structured = map[string]any{"scope": scope, "output": string(b), "source": g.backendURL}
			}
		}
		writeMCPToolResult(w, id, false, structured, "ui://mcp-demo/inventory-view")

	case "get_status":
		g.mu.RLock()
		var pCount, aCount, rCount int
		for _, item := range g.pending {
			switch item.Status {
			case StatusPending:
				pCount++
			case StatusApproved:
				aCount++
			case StatusRejected:
				rCount++
			}
		}
		g.mu.RUnlock()
		structured := map[string]any{
			"gateway":    "mcp-enforcement-gateway",
			"version":    "2.0.0",
			"backendURL": g.backendURL,
			"runnerURL":  g.runnerURL,
			"counts": map[string]int{
				"pending":  pCount,
				"approved": aCount,
				"rejected": rCount,
			},
			"layers": []string{
				"Layer 1: Go MCP Approval Gateway (PENDING_APPROVAL gate)",
				"Layer 2: Cilium CiliumNetworkPolicy (L3/L4 egress lockdown + Hubble UI)",
				"Layer 3: Tetragon TracingPolicyNamespaced (sys_execve -> SIGKILL)",
			},
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}
		writeMCPToolResult(w, id, false, structured, "ui://mcp-demo/security-posture")

	case "fetch_url":
		targetURL, _ := params.Arguments["url"].(string)
		if strings.TrimSpace(targetURL) == "" {
			writeJSONRPCError(w, id, -32602, "fetch_url requires arguments.url")
			return
		}
		if g.runnerURL == "" {
			writeJSONRPCError(w, id, -32013, "RUNNER_URL is not configured")
			return
		}
		res, err := g.callRunnerFetch(targetURL)
		if err != nil {
			writeJSONRPCError(w, id, -32014, "mcp-tool-runner unreachable: "+err.Error())
			return
		}
		structured := map[string]any{
			"url":          res.URL,
			"allowed":      res.Allowed,
			"status_code":  res.StatusCode,
			"duration_ms":  res.DurationMS,
			"body_preview": res.BodyPreview,
			"error":        res.Error,
			"pod":          res.Pod,
			"layer":        "Layer 2 (Cilium Network Policy)",
		}
		writeMCPToolResult(w, id, !res.Allowed, structured, "ui://mcp-demo/egress-result")

	case "exec_shell":
		cmdStr, _ := params.Arguments["command"].(string)
		if strings.TrimSpace(cmdStr) == "" {
			cmdStr = "id"
		}
		if g.runnerURL == "" {
			writeJSONRPCError(w, id, -32013, "RUNNER_URL is not configured")
			return
		}
		res, err := g.callRunnerShell(cmdStr)
		if err != nil {
			writeJSONRPCError(w, id, -32015, "mcp-tool-runner unreachable: "+err.Error())
			return
		}
		structured := map[string]any{
			"command":           res.Command,
			"exit_code":         res.ExitCode,
			"signal":            res.Signal,
			"killed_by_sigkill": res.KilledBySigkill,
			"output":            res.Output,
			"pod":               res.Pod,
			"layer":             "Layer 3 (Tetragon eBPF Runtime)",
		}
		writeMCPToolResult(w, id, res.ExitCode != 0, structured, "ui://mcp-demo/runtime-result")

	default:
		g.proxyToBackend(w, rawBody, id, name)
	}
}

type RunnerFetchResponse struct {
	URL         string `json:"url"`
	Allowed     bool   `json:"allowed"`
	StatusCode  int    `json:"status_code"`
	DurationMS  int64  `json:"duration_ms"`
	BodyPreview string `json:"body_preview,omitempty"`
	Body        string `json:"body,omitempty"`
	Error       string `json:"error,omitempty"`
	Pod         string `json:"pod"`
}

type RunnerShellResponse struct {
	Command         string `json:"command"`
	ExitCode        int    `json:"exit_code"`
	Signal          string `json:"signal,omitempty"`
	KilledBySigkill bool   `json:"killed_by_sigkill"`
	Output          string `json:"output"`
	Pod             string `json:"pod"`
}

func (g *Gateway) callRunnerFetch(targetURL string) (*RunnerFetchResponse, error) {
	payload, _ := json.Marshal(map[string]string{"url": targetURL})
	resp, err := g.client.Post(g.runnerURL+"/run/fetch", "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out RunnerFetchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (g *Gateway) callRunnerShell(command string) (*RunnerShellResponse, error) {
	payload, _ := json.Marshal(map[string]string{"command": command})
	resp, err := g.client.Post(g.runnerURL+"/run/shell", "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out RunnerShellResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (g *Gateway) proxyToBackend(w http.ResponseWriter, body []byte, id any, toolName string) {
	req, err := http.NewRequest(http.MethodPost, g.backendURL+"/", bytes.NewReader(body))
	if err != nil {
		writeJSONRPCError(w, id, -32010, "failed to create backend request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		writeJSONRPCError(w, id, -32011, "backend unreachable: "+err.Error())
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		writeJSONRPCError(w, id, -32012, "failed to read backend response")
		return
	}

	var structured map[string]any
	if json.Valid(respBody) {
		_ = json.Unmarshal(respBody, &structured)
	}
	if structured == nil {
		structured = map[string]any{
			"proxied": true,
			"action":  toolName,
			"backend": g.backendURL,
			"output":  strings.TrimSpace(string(respBody)),
		}
	}
	writeMCPToolResult(w, id, false, structured, "ui://mcp-demo/security-posture")
}

func writeMCPToolResult(w http.ResponseWriter, id any, isError bool, structured map[string]any, uiURI string) {
	pretty, _ := json.MarshalIndent(structured, "", "  ")
	result := map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": string(pretty),
			},
		},
		"structuredContent": structured,
		"isError":           isError,
	}
	if uiURI != "" {
		result["_meta"] = map[string]any{
			"ui": map[string]any{"resourceUri": uiURI},
		}
	}
	writeJSONRPCResult(w, id, result)
}

func (g *Gateway) handlePending(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]*PendingCall, 0, len(g.pending))
	for _, p := range g.pending {
		out = append(out, p)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"pending": out})
}

func (g *Gateway) handleDecision(w http.ResponseWriter, r *http.Request, status ApprovalStatus) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id query parameter", http.StatusBadRequest)
		return
	}
	g.mu.Lock()
	pc, ok := g.pending[id]
	if !ok {
		g.mu.Unlock()
		http.Error(w, "unknown approval id", http.StatusNotFound)
		return
	}
	if pc.Status != StatusPending {
		g.mu.Unlock()
		http.Error(w, "request is not pending", http.StatusConflict)
		return
	}
	pc.Status = status
	pc.UpdatedAt = time.Now().UTC()
	g.mu.Unlock()

	select {
	case pc.decision <- status:
	default:
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "status": status, "tool": pc.Tool})
}

func writeJSONRPCResult(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}

func writeJSONRPCError(w http.ResponseWriter, id any, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func newGatewayMux(gw *Gateway) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", gw.handleConsole)
	mux.HandleFunc("/mcp", gw.handleMCP)
	mux.HandleFunc("/pending", gw.handlePending)
	mux.HandleFunc("/approve", func(w http.ResponseWriter, r *http.Request) { gw.handleDecision(w, r, StatusApproved) })
	mux.HandleFunc("/reject", func(w http.ResponseWriter, r *http.Request) { gw.handleDecision(w, r, StatusRejected) })
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	return mux
}

// --- RUNNER MODE (executes inside mcp-tool-runner pod: subject to Cilium & Tetragon) ---

func newRunnerMux() *http.ServeMux {
	podName := envOr("POD_NAME", "mcp-tool-runner")
	httpClient := &http.Client{Timeout: 5 * time.Second}

	mux := http.NewServeMux()
	mux.HandleFunc("/run/fetch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil || req.URL == "" {
			http.Error(w, "missing url", http.StatusBadRequest)
			return
		}
		start := time.Now()
		resp, err := httpClient.Get(req.URL)
		dur := time.Since(start).Milliseconds()

		out := RunnerFetchResponse{
			URL:        req.URL,
			DurationMS: dur,
			Pod:        podName,
		}
		if err != nil {
			out.Allowed = false
			out.Error = err.Error()
		} else {
			defer resp.Body.Close()
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			out.Allowed = true
			out.StatusCode = resp.StatusCode
			out.Body = string(b)
			preview := strings.TrimSpace(string(b))
			if len(preview) > 300 {
				preview = preview[:300] + "..."
			}
			out.BodyPreview = preview
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	mux.HandleFunc("/run/shell", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Command string `json:"command"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil || req.Command == "" {
			req.Command = "id"
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", req.Command)
		rawOut, err := cmd.CombinedOutput()

		res := RunnerShellResponse{
			Command: req.Command,
			Output:  strings.TrimSpace(string(rawOut)),
			Pod:     podName,
		}
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
					sig := ws.Signal()
					res.Signal = sig.String()
					res.ExitCode = 128 + int(sig)
					if sig == syscall.SIGKILL {
						res.KilledBySigkill = true
						if res.Output == "" {
							res.Output = "killed by Linux kernel SIGKILL (exit 137) — Tetragon runtime policy enforced"
						}
					}
				} else {
					res.ExitCode = exitErr.ExitCode()
				}
			} else {
				res.ExitCode = 1
				res.Output = err.Error()
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	return mux
}

// --- BACKEND MODE (executes inside approved-backend pod) ---

func newBackendMux() *http.ServeMux {
	mux := http.NewServeMux()
	inventoryHandler := func(w http.ResponseWriter, r *http.Request) {
		scope := r.URL.Query().Get("scope")
		if scope == "" {
			scope = "demo"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service": "approved-backend",
			"scope":   scope,
			"status":  "ok",
			"items": []map[string]any{
				{"name": "mcp-gateway", "namespace": "mcp-demo", "tier": "layer-1-app", "status": "healthy", "endpoint": ":8090 (NodePort 30090)"},
				{"name": "mcp-tool-runner", "namespace": "mcp-demo", "tier": "layer-2-3-sandbox", "status": "healthy", "endpoint": "mcp-tool-runner:8081"},
				{"name": "approved-backend", "namespace": "mcp-demo", "tier": "internal-api", "status": "healthy", "endpoint": "approved-backend:8080"},
				{"name": "hubble-ui", "namespace": "kube-system", "tier": "observability", "status": "healthy", "endpoint": "NodePort 31235"},
			},
		})
	}
	mux.HandleFunc("/inventory", inventoryHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			inventoryHandler(w, r)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var req JSONRPCRequest
		_ = json.Unmarshal(body, &req)
		var params ToolCallParams
		_ = json.Unmarshal(req.Params, &params)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service":   "approved-backend",
			"status":    "executed_after_approval",
			"action":    params.Name,
			"arguments": params.Arguments,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	return mux
}

func main() {
	modeFlag := flag.String("mode", envOr("MODE", "gateway"), "Server mode: gateway, runner, or backend")
	flag.Parse()

	mode := strings.ToLower(strings.TrimSpace(*modeFlag))
	var (
		addr string
		mux  *http.ServeMux
	)

	switch mode {
	case "gateway":
		addr = envOr("LISTEN_ADDR", ":8090")
		backend := envOr("BACKEND_URL", "http://approved-backend.mcp-demo.svc.cluster.local:8080")
		runner := envOr("RUNNER_URL", "http://mcp-tool-runner.mcp-demo.svc.cluster.local:8081")
		gw := NewGateway(backend, runner)
		mux = newGatewayMux(gw)
		log.Printf("mcp-gateway listening on %s (backend=%s, runner=%s)", addr, backend, runner)
	case "runner":
		addr = envOr("LISTEN_ADDR", ":8081")
		mux = newRunnerMux()
		log.Printf("mcp-tool-runner listening on %s", addr)
	case "backend":
		addr = envOr("LISTEN_ADDR", ":8080")
		mux = newBackendMux()
		log.Printf("approved-backend listening on %s", addr)
	default:
		log.Fatalf("unknown mode %q (expected gateway, runner, or backend)", mode)
	}

	if err := http.ListenAndServe(addr, mux); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
