package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SoftenTheGlare382/oa-gate/internal/adapter"
	"github.com/SoftenTheGlare382/oa-gate/internal/config"
	"github.com/SoftenTheGlare382/oa-gate/internal/gateway"
)

type fakeAdapter struct{ decideCalls int }

func (f *fakeAdapter) Name() string { return "fake" }
func (f *fakeAdapter) SendMessage(ctx context.Context, target, text string) (string, error) {
	return "msg-1", nil
}
func (f *fakeAdapter) ListApprovals(ctx context.Context, status string) ([]adapter.Approval, error) {
	return []adapter.Approval{{ID: "a1", Platform: "fake", Status: "pending"}}, nil
}
func (f *fakeAdapter) GetApproval(ctx context.Context, id string) (adapter.Approval, error) {
	return adapter.Approval{ID: id, Platform: "fake", Status: "pending"}, nil
}
func (f *fakeAdapter) DecideApproval(ctx context.Context, id, decision, comment string) (string, error) {
	f.decideCalls++
	return "receipt", nil
}
func (f *fakeAdapter) ListCalendar(ctx context.Context, start, end time.Time) ([]adapter.CalendarEvent, error) {
	return nil, nil
}
func (f *fakeAdapter) SearchDocs(ctx context.Context, query string) ([]adapter.Doc, error) {
	return nil, nil
}
func (f *fakeAdapter) ReadDoc(ctx context.Context, id string) (adapter.Doc, error) {
	return adapter.Doc{}, nil
}

func testGateway(t *testing.T) *gateway.Gateway {
	t.Helper()
	cfg := config.Default()
	cfg.DryRun = false
	cfg.AuditDB = filepath.Join(t.TempDir(), "audit.jsonl")
	g, err := gateway.NewWithAdapters(cfg, map[string]adapter.Adapter{"fake": &fakeAdapter{}})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestHTTPDecideRequiresConfirm(t *testing.T) {
	h := NewHTTPHandler(testGateway(t))
	req := httptest.NewRequest("POST", "/tools/office.approval.decide",
		strings.NewReader(`{"platform":"fake","approval_id":"a1","decision":"approve","confirm":false,"idempotency_key":"k1"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without confirm, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHTTPToolsList(t *testing.T) {
	h := NewHTTPHandler(testGateway(t))
	req := httptest.NewRequest("GET", "/tools", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "office.approval.decide") {
		t.Fatalf("tools list unexpected: %d %s", rec.Code, rec.Body.String())
	}
}

func TestMCPToolsListAndCall(t *testing.T) {
	g := testGateway(t)
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n" +
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"office.approval.list","arguments":{"platform":"fake"}}}` + "\n")
	var out bytes.Buffer
	if err := ServeMCP(context.Background(), g, in, &out); err != nil {
		t.Fatal(err)
	}
	rawOut := out.String()
	dec := json.NewDecoder(strings.NewReader(rawOut))
	for i := 0; i < 3; i++ {
		var resp map[string]any
		if err := dec.Decode(&resp); err != nil {
			t.Fatalf("decode response %d: %v", i, err)
		}
		if resp["error"] != nil {
			t.Fatalf("response %d has error: %v", i, resp["error"])
		}
	}
	if !strings.Contains(rawOut, "office.approval.decide") {
		t.Fatalf("tools/list output missing decide tool: %s", rawOut)
	}
}
