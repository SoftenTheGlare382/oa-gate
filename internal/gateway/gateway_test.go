package gateway

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/SoftenTheGlare382/oa-gate/internal/adapter"
	"github.com/SoftenTheGlare382/oa-gate/internal/config"
)

type fakeAdapter struct {
	decideCalls int
	approvals   []adapter.Approval
}

func (f *fakeAdapter) Name() string { return "fake" }
func (f *fakeAdapter) SendMessage(ctx context.Context, target, text string) (string, error) {
	return "msg-1", nil
}
func (f *fakeAdapter) ListApprovals(ctx context.Context, status string) ([]adapter.Approval, error) {
	return f.approvals, nil
}
func (f *fakeAdapter) GetApproval(ctx context.Context, id string) (adapter.Approval, error) {
	return adapter.Approval{ID: id, Platform: "fake", Title: "t", Status: "pending"}, nil
}
func (f *fakeAdapter) DecideApproval(ctx context.Context, id, decision, comment string) (string, error) {
	f.decideCalls++
	return "receipt-" + id, nil
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

func testGateway(t *testing.T, dryRun bool) (*Gateway, *fakeAdapter) {
	t.Helper()
	cfg := config.Default()
	cfg.DryRun = dryRun
	cfg.AuditDB = filepath.Join(t.TempDir(), "audit.jsonl")
	f := &fakeAdapter{approvals: []adapter.Approval{
		{ID: "old", Platform: "fake", CreatedAt: time.Now().Add(-time.Hour)},
		{ID: "new", Platform: "fake", CreatedAt: time.Now()},
	}}
	g, err := NewWithAdapters(cfg, map[string]adapter.Adapter{"fake": f})
	if err != nil {
		t.Fatal(err)
	}
	return g, f
}

func TestDryRunNeverCallsAdapter(t *testing.T) {
	g, f := testGateway(t, true)
	ctx := context.Background()
	if _, err := g.SendMessage(ctx, "fake", "u1", "hi"); err != nil {
		t.Fatal(err)
	}
	res, err := g.DecideApproval(ctx, "fake", "a1", "approve", "", true, "k1")
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || f.decideCalls != 0 {
		t.Fatalf("dry-run must not call adapter: res=%+v calls=%d", res, f.decideCalls)
	}
}

func TestDecideRequiresConfirmAndKey(t *testing.T) {
	g, _ := testGateway(t, false)
	ctx := context.Background()
	if _, err := g.DecideApproval(ctx, "fake", "a1", "approve", "", false, "k1"); err == nil {
		t.Fatal("decide without confirm must fail")
	}
	if _, err := g.DecideApproval(ctx, "fake", "a1", "approve", "", true, ""); err == nil {
		t.Fatal("decide without idempotency key must fail")
	}
	if _, err := g.DecideApproval(ctx, "fake", "a1", "maybe", "", true, "k1"); err == nil {
		t.Fatal("invalid decision must fail")
	}
}

func TestDecideIdempotencyReplays(t *testing.T) {
	g, f := testGateway(t, false)
	ctx := context.Background()
	first, err := g.DecideApproval(ctx, "fake", "a1", "approve", "", true, "k9")
	if err != nil {
		t.Fatal(err)
	}
	second, err := g.DecideApproval(ctx, "fake", "a1", "approve", "", true, "k9")
	if err != nil {
		t.Fatal(err)
	}
	if f.decideCalls != 1 {
		t.Fatalf("adapter decide must be called once, got %d", f.decideCalls)
	}
	if !second.Replayed || second.Receipt != first.Receipt {
		t.Fatalf("second call must replay first result: %+v vs %+v", second, first)
	}
}

func TestListApprovalsSortedNewestFirst(t *testing.T) {
	g, _ := testGateway(t, false)
	list, err := g.ListApprovals(context.Background(), "fake", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != "new" {
		t.Fatalf("expected newest first, got %+v", list)
	}
}
