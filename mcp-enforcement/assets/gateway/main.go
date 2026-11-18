package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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

type Gateway struct {
	backendURL string
	client     *http.Client

	mu      sync.RWMutex
	pending map[string]*PendingCall
	counter uint64
}

func NewGateway(backendURL string) *Gateway {
	return &Gateway{
		backendURL: backendURL,
		client:     &http.Client{Timeout: 30 * time.Second},
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
	case "drop_database", "modify_infra", "exec_shell":
		return true
	default:
		return false
	}
}

func (g *Gateway) handleMCP(w http.ResponseWriter, r *http.Request) {
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

	if req.Method != "tools/call" {
		// Forward non-tool-call methods as-is
		g.proxyToBackend(w, body, req.ID)
		return
	}

	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil || strings.TrimSpace(params.Name) == "" {
		writeJSONRPCError(w, req.ID, -32602, "invalid params: tools/call requires params.name")
		return
	}

	tool := params.Name
	log.Printf("[gateway] tools/call name=%s id=%v", tool, req.ID)

	switch {
	case isLowRisk(tool):
		g.proxyToBackend(w, body, req.ID)
		return
	case isHighRisk(tool):
		g.handleHighRisk(w, r, req, body, tool)
		return
	default:
		// Treat unknown tools as high-risk by default (fail-safe)
		log.Printf("[gateway] unknown tool %q treated as high-risk", tool)
		g.handleHighRisk(w, r, req, body, tool)
		return
	}
}

func (g *Gateway) handleHighRisk(w http.ResponseWriter, r *http.Request, req JSONRPCRequest, raw []byte, tool string) {
	approvalID := g.nextID()
	pc := &PendingCall{
		ID:         approvalID,
		Tool:       tool,
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

	log.Printf("[gateway] high-risk call held: approval_id=%s tool=%s", approvalID, tool)

	// Wait for approval/rejection or client disconnect
	select {
	case decision := <-pc.decision:
		switch decision {
		case StatusApproved:
			log.Printf("[gateway] approval_id=%s approved; forwarding", approvalID)
			g.proxyToBackend(w, raw, req.ID)
		case StatusRejected:
			log.Printf("[gateway] approval_id=%s rejected", approvalID)
			writeJSONRPCError(w, req.ID, -32001, fmt.Sprintf("tool call rejected by approver (approval_id=%s)", approvalID))
		default:
			writeJSONRPCError(w, req.ID, -32002, "unknown approval decision")
		}
		// keep record for audit; mark finalized already set
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

func (g *Gateway) proxyToBackend(w http.ResponseWriter, body []byte, id any) {
	req, err := http.NewRequest(http.MethodPost, g.backendURL, bytes.NewReader(body))
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

	// If backend returns non-JSON (e.g. http-echo plain text), wrap into JSON-RPC result
	if !json.Valid(respBody) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]any{
				"proxied": true,
				"backend": g.backendURL,
				"output":  string(respBody),
			},
		})
		return
	}

	// Pass through valid JSON; if it's not a JSON-RPC envelope, wrap it
	var probe map[string]any
	_ = json.Unmarshal(respBody, &probe)
	if _, ok := probe["jsonrpc"]; ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  probe,
	})
}

func (g *Gateway) handlePending(w http.ResponseWriter, r *http.Request) {
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

	// notify waiter (non-blocking)
	select {
	case pc.decision <- status:
	default:
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "status": status, "tool": pc.Tool})
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

func main() {
	addr := envOr("LISTEN_ADDR", ":8090")
	backend := envOr("BACKEND_URL", "http://approved-backend.mcp-demo.svc.cluster.local:8080")
	gw := NewGateway(backend)

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", gw.handleMCP)
	mux.HandleFunc("/pending", gw.handlePending)
	mux.HandleFunc("/approve", func(w http.ResponseWriter, r *http.Request) { gw.handleDecision(w, r, StatusApproved) })
	mux.HandleFunc("/reject", func(w http.ResponseWriter, r *http.Request) { gw.handleDecision(w, r, StatusRejected) })
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })

	log.Printf("mcp-gateway listening on %s, backend=%s", addr, backend)
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
