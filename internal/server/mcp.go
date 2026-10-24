package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/SoftenTheGlare382/oa-gate/internal/gateway"
)

// Minimal MCP (Model Context Protocol) over stdio, JSON-RPC 2.0.
// Supports: initialize, notifications/initialized, ping, tools/list,
// tools/call. Anything else returns a JSON-RPC method-not-found error.
// This is intentionally small and dependency-free; it speaks the same
// tool surface as the HTTP API, so policy cannot drift between transports.

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ServeMCP runs the stdio loop until EOF.
func ServeMCP(ctx context.Context, g *gateway.Gateway, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	enc := json.NewEncoder(out)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		// Notifications have no id and get no response.
		if len(req.ID) == 0 {
			continue
		}
		resp, err := handleMCP(ctx, g, req)
		if err != nil {
			resp = rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: err.Error()}}
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func handleMCP(ctx context.Context, g *gateway.Gateway, req rpcRequest) (rpcResponse, error) {
	base := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		base.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "oa-gate", "version": "0.1.0"},
		}
		return base, nil
	case "ping":
		base.Result = map[string]any{}
		return base, nil
	case "tools/list":
		tools := make([]map[string]any, 0, len(ToolNames()))
		for _, name := range ToolNames() {
			tools = append(tools, map[string]any{"name": name, "description": "oa-gate unified office tool: " + name, "inputSchema": map[string]any{"type": "object"}})
		}
		base.Result = map[string]any{"tools": tools}
		return base, nil
	case "tools/call":
		var params struct {
			Name      string                     `json:"name"`
			Arguments map[string]json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return base, fmt.Errorf("invalid tools/call params: %w", err)
		}
		res, err := CallTool(ctx, g, params.Name, params.Arguments)
		if err != nil {
			base.Result = map[string]any{"content": []map[string]any{{"type": "text", "text": "error: " + err.Error()}}, "isError": true}
			return base, nil
		}
		raw, _ := json.Marshal(res)
		base.Result = map[string]any{"content": []map[string]any{{"type": "text", "text": string(raw)}}}
		return base, nil
	default:
		return base, fmt.Errorf("method not found: %s", req.Method)
	}
}
