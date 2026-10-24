// Package gateway is the policy layer: it sits between the transports
// (HTTP, MCP) and the platform adapters, and it owns every safety rule.
// An adapter is never called for a write before these checks pass.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/SoftenTheGlare382/oa-gate/internal/adapter"
	"github.com/SoftenTheGlare382/oa-gate/internal/config"
)

// Gateway holds configuration, adapters, audit and idempotency stores.
type Gateway struct {
	cfg       config.Config
	adapters  map[string]adapter.Adapter
	audit     *AuditStore
	idem      *IdempotencyStore
	allowUser map[string]bool
	allowGrp  map[string]bool
}

// New builds a gateway from config, wiring only enabled platforms.
func New(cfg config.Config) (*Gateway, error) {
	g := &Gateway{
		cfg:       cfg,
		adapters:  map[string]adapter.Adapter{},
		allowUser: map[string]bool{},
		allowGrp:  map[string]bool{},
	}
	for _, u := range cfg.AllowUsers {
		g.allowUser[u] = true
	}
	for _, gr := range cfg.AllowGroups {
		g.allowGrp[gr] = true
	}
	for name, p := range cfg.Platforms {
		if !p.Enabled {
			continue
		}
		switch name {
		case "feishu":
			g.adapters[name] = adapter.NewFeishu(p.AppID, p.AppSecret, p.BaseURL, p.UserID, p.CalendarID)
		case "dingtalk":
			g.adapters[name] = adapter.NewDingTalk(p.AppKey, p.AppSecret, p.AgentID, p.ProcessCode, p.BaseURL)
		case "wecom":
			g.adapters[name] = adapter.NewWeCom(p.CorpID, p.AgentID, p.Secret, p.BaseURL)
		default:
			return nil, fmt.Errorf("unknown platform in config: %s", name)
		}
	}
	audit, err := NewAuditStore(cfg.AuditDB)
	if err != nil {
		return nil, err
	}
	idem, err := NewIdempotencyStore(cfg.AuditDB + ".idempotency.json")
	if err != nil {
		return nil, err
	}
	g.audit = audit
	g.idem = idem
	return g, nil
}

// NewWithAdapters is for tests: inject fake adapters directly.
func NewWithAdapters(cfg config.Config, ads map[string]adapter.Adapter) (*Gateway, error) {
	g := &Gateway{cfg: cfg, adapters: ads, allowUser: map[string]bool{}, allowGrp: map[string]bool{}}
	audit, err := NewAuditStore(cfg.AuditDB)
	if err != nil {
		return nil, err
	}
	idem, err := NewIdempotencyStore(cfg.AuditDB + ".idempotency.json")
	if err != nil {
		return nil, err
	}
	g.audit = audit
	g.idem = idem
	return g, nil
}

func (g *Gateway) Config() config.Config { return g.cfg }

func (g *Gateway) adapterFor(platform string) (adapter.Adapter, error) {
	a, ok := g.adapters[platform]
	if !ok {
		return nil, fmt.Errorf("platform %s is not enabled in config", platform)
	}
	return a, nil
}

// MessageResult is the normalized result of office.message.send.
type MessageResult struct {
	Platform  string `json:"platform"`
	MessageID string `json:"message_id"`
	DryRun    bool   `json:"dry_run"`
}

// SendMessage sends a message, or simulates it when dry-run is on.
// A dry-run never calls the platform adapter.
func (g *Gateway) SendMessage(ctx context.Context, platform, target, text string) (MessageResult, error) {
	if target == "" || text == "" {
		return MessageResult{}, errors.New("target and text are required")
	}
	if g.cfg.DryRun {
		g.audit.Append(AuditEntry{Time: time.Now(), Tool: "office.message.send", Platform: platform, Target: target, DryRun: true})
		return MessageResult{Platform: platform, MessageID: "dry-run", DryRun: true}, nil
	}
	a, err := g.adapterFor(platform)
	if err != nil {
		return MessageResult{}, err
	}
	id, err := a.SendMessage(ctx, target, text)
	if err != nil {
		return MessageResult{}, err
	}
	g.audit.Append(AuditEntry{Time: time.Now(), Tool: "office.message.send", Platform: platform, Target: target, Receipt: id})
	return MessageResult{Platform: platform, MessageID: id}, nil
}

// ListApprovals aggregates pending (or filtered) approvals across all
// enabled platforms, or a single platform when platform != "".
// Results are sorted newest first when CreatedAt is available.
func (g *Gateway) ListApprovals(ctx context.Context, platform, status string) ([]adapter.Approval, error) {
	var names []string
	if platform != "" {
		if _, err := g.adapterFor(platform); err != nil {
			return nil, err
		}
		names = []string{platform}
	} else {
		for name := range g.adapters {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	var out []adapter.Approval
	for _, name := range names {
		list, err := g.adapters[name].ListApprovals(ctx, status)
		if err != nil {
			// A platform that is not configured for approvals (missing
			// user_id / process_code) must not poison the whole list:
			// skip it with its reason recorded in the error only when
			// it was the only platform requested.
			if platform != "" {
				return nil, err
			}
			continue
		}
		out = append(out, list...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// GetApproval reads one approval from one platform.
func (g *Gateway) GetApproval(ctx context.Context, platform, id string) (adapter.Approval, error) {
	a, err := g.adapterFor(platform)
	if err != nil {
		return adapter.Approval{}, err
	}
	return a.GetApproval(ctx, id)
}

// DecideResult is the normalized result of office.approval.decide.
type DecideResult struct {
	Platform       string `json:"platform"`
	ApprovalID     string `json:"approval_id"`
	Decision       string `json:"decision"`
	Receipt        string `json:"receipt"`
	DryRun         bool   `json:"dry_run"`
	IdempotencyKey string `json:"idempotency_key"`
	Replayed       bool   `json:"replayed"`
}

// DecideApproval is the guarded write path. Order matters:
//  1. validate decision / confirm / idempotency key
//  2. replay a stored result for a repeated key (no platform call)
//  3. dry-run preview (no platform call, key is NOT consumed)
//  4. real platform call, then store + audit
func (g *Gateway) DecideApproval(ctx context.Context, platform, id, decision, comment string, confirm bool, idemKey string) (DecideResult, error) {
	if decision != "approve" && decision != "reject" {
		return DecideResult{}, errors.New("decision must be approve or reject")
	}
	if id == "" {
		return DecideResult{}, errors.New("approval id is required")
	}
	if g.cfg.RequireConfirm && !confirm {
		return DecideResult{}, errors.New("office.approval.decide requires confirm=true")
	}
	if idemKey == "" {
		return DecideResult{}, errors.New("office.approval.decide requires idempotency_key")
	}
	if stored, ok := g.idem.Get(idemKey); ok {
		var res DecideResult
		if err := json.Unmarshal(stored, &res); err == nil {
			res.Replayed = true
			return res, nil
		}
	}
	if g.cfg.DryRun {
		g.audit.Append(AuditEntry{Time: time.Now(), Tool: "office.approval.decide", Platform: platform, ApprovalID: id, Decision: decision, IdempotencyKey: idemKey, DryRun: true})
		return DecideResult{Platform: platform, ApprovalID: id, Decision: decision, Receipt: "dry-run", DryRun: true, IdempotencyKey: idemKey}, nil
	}
	a, err := g.adapterFor(platform)
	if err != nil {
		return DecideResult{}, err
	}
	receipt, err := a.DecideApproval(ctx, id, decision, comment)
	if err != nil {
		// Failed decisions do NOT consume the idempotency key, so the
		// caller can fix the cause and retry with the same key.
		return DecideResult{}, err
	}
	res := DecideResult{Platform: platform, ApprovalID: id, Decision: decision, Receipt: receipt, IdempotencyKey: idemKey}
	raw, _ := json.Marshal(res)
	_ = g.idem.Put(idemKey, raw)
	g.audit.Append(AuditEntry{Time: time.Now(), Tool: "office.approval.decide", Platform: platform, ApprovalID: id, Decision: decision, IdempotencyKey: idemKey, Receipt: receipt})
	return res, nil
}

// ListCalendar lists events from one platform (read-only).
func (g *Gateway) ListCalendar(ctx context.Context, platform string, start, end time.Time) ([]adapter.CalendarEvent, error) {
	a, err := g.adapterFor(platform)
	if err != nil {
		return nil, err
	}
	return a.ListCalendar(ctx, start, end)
}

// SearchDocs searches documents on one platform (read-only).
func (g *Gateway) SearchDocs(ctx context.Context, platform, query string) ([]adapter.Doc, error) {
	a, err := g.adapterFor(platform)
	if err != nil {
		return nil, err
	}
	return a.SearchDocs(ctx, query)
}

// ReadDoc reads one document from one platform (read-only).
func (g *Gateway) ReadDoc(ctx context.Context, platform, id string) (adapter.Doc, error) {
	a, err := g.adapterFor(platform)
	if err != nil {
		return adapter.Doc{}, err
	}
	return a.ReadDoc(ctx, id)
}

// --- audit store (JSON lines, append-only) ---

// AuditEntry is one audited action.
type AuditEntry struct {
	Time           time.Time `json:"time"`
	Tool           string    `json:"tool"`
	Platform       string    `json:"platform,omitempty"`
	Target         string    `json:"target,omitempty"`
	ApprovalID     string    `json:"approval_id,omitempty"`
	Decision       string    `json:"decision,omitempty"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	Receipt        string    `json:"receipt,omitempty"`
	DryRun         bool      `json:"dry_run"`
}

// AuditStore appends entries to a file. It never overwrites history.
type AuditStore struct {
	mu   sync.Mutex
	path string
}

func NewAuditStore(path string) (*AuditStore, error) {
	if path == "" {
		return nil, errors.New("audit_db path is required")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	return &AuditStore{path: path}, nil
}

func (s *AuditStore) Append(e AuditEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	raw, _ := json.Marshal(e)
	_, _ = f.Write(append(raw, '\n'))
}

// --- idempotency store (JSON map, persisted on every put) ---

type IdempotencyStore struct {
	mu   sync.Mutex
	path string
	data map[string]json.RawMessage
}

func NewIdempotencyStore(path string) (*IdempotencyStore, error) {
	s := &IdempotencyStore{path: path, data: map[string]json.RawMessage{}}
	raw, err := os.ReadFile(path)
	if err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &s.data)
	}
	return s, nil
}

func (s *IdempotencyStore) Get(key string) (json.RawMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	return v, ok
}

func (s *IdempotencyStore) Put(key string, val json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = val
	raw, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
