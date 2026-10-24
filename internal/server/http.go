// Package server exposes the gateway over HTTP JSON and over MCP stdio.
// Both transports call the same Gateway methods, so policy is identical
// no matter how the agent connects.
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/SoftenTheGlare382/oa-gate/internal/gateway"
)

// NewHTTPHandler returns the HTTP API handler.
func NewHTTPHandler(g *gateway.Gateway) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/tools", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"tools": ToolNames()})
	})
	mux.HandleFunc("/tools/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "only POST is allowed")
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/tools/")
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}
		res, err := CallTool(r.Context(), g, name, body)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": res})
	})
	return mux
}

// ToolNames lists every unified tool.
func ToolNames() []string {
	return []string{
		"office.message.send",
		"office.approval.list",
		"office.approval.get",
		"office.approval.decide",
		"office.calendar.list",
		"office.doc.search",
		"office.doc.read",
	}
}

func strArg(body map[string]json.RawMessage, k string) string {
	var s string
	_ = json.Unmarshal(body[k], &s)
	return s
}

func boolArg(body map[string]json.RawMessage, k string) bool {
	var b bool
	_ = json.Unmarshal(body[k], &b)
	return b
}

// CallTool dispatches one unified tool call. It is shared by HTTP and MCP.
func CallTool(ctx context.Context, g *gateway.Gateway, name string, body map[string]json.RawMessage) (any, error) {
	switch name {
	case "office.message.send":
		return g.SendMessage(ctx, strArg(body, "platform"), strArg(body, "target"), strArg(body, "text"))
	case "office.approval.list":
		return g.ListApprovals(ctx, strArg(body, "platform"), strArg(body, "status"))
	case "office.approval.get":
		return g.GetApproval(ctx, strArg(body, "platform"), strArg(body, "approval_id"))
	case "office.approval.decide":
		return g.DecideApproval(ctx, strArg(body, "platform"), strArg(body, "approval_id"),
			strArg(body, "decision"), strArg(body, "comment"), boolArg(body, "confirm"), strArg(body, "idempotency_key"))
	case "office.calendar.list":
		start, _ := time.Parse(time.RFC3339, strArg(body, "start"))
		end, _ := time.Parse(time.RFC3339, strArg(body, "end"))
		return g.ListCalendar(ctx, strArg(body, "platform"), start, end)
	case "office.doc.search":
		return g.SearchDocs(ctx, strArg(body, "platform"), strArg(body, "query"))
	case "office.doc.read":
		return g.ReadDoc(ctx, strArg(body, "platform"), strArg(body, "doc_id"))
	default:
		return nil, errUnknownTool(name)
	}
}

type unknownToolError string

func (e unknownToolError) Error() string { return "unknown tool: " + string(e) }

func errUnknownTool(name string) error { return unknownToolError(name) }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"ok": false, "error": msg})
}
