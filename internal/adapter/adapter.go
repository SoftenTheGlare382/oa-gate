// Package adapter defines the unified tool surface and the per-platform
// implementations behind it. The gateway never talks to a platform API
// directly; it only calls an Adapter, so a platform can be replaced or
// mocked without touching policy, audit or idempotency code.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrNotImplemented is returned when a platform does not (yet) support a
// tool, or when the configuration needed for it is missing. It is never
// disguised as a success, and the gateway never retries a decide on it.
var ErrNotImplemented = errors.New("not implemented for this platform")

// NotImplementedf builds a clear, actionable ErrNotImplemented.
func NotImplementedf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrNotImplemented, fmt.Sprintf(format, args...))
}

// Approval is the normalized approval shape every platform maps to.
type Approval struct {
	ID        string            `json:"id"`
	Platform  string            `json:"platform"`
	Title     string            `json:"title"`
	Applicant string            `json:"applicant"`
	Status    string            `json:"status"` // pending | approved | rejected | unknown
	CreatedAt time.Time         `json:"created_at,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
}

// CalendarEvent is a normalized calendar event (read-only in v0.1).
type CalendarEvent struct {
	ID        string    `json:"id"`
	Platform  string    `json:"platform"`
	Title     string    `json:"title"`
	StartTime time.Time `json:"start_time,omitempty"`
	EndTime   time.Time `json:"end_time,omitempty"`
}

// Doc is a normalized document (read-only in v0.1).
type Doc struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
	Title    string `json:"title"`
	Content  string `json:"content,omitempty"`
}

// Adapter is one platform (feishu, dingtalk or wecom).
type Adapter interface {
	Name() string
	SendMessage(ctx context.Context, target, text string) (string, error)
	ListApprovals(ctx context.Context, status string) ([]Approval, error)
	GetApproval(ctx context.Context, id string) (Approval, error)
	// DecideApproval executes a real approval decision. It must only be
	// called by the gateway after confirmation, dry-run and idempotency
	// checks have passed. It returns a platform receipt string.
	DecideApproval(ctx context.Context, id, decision, comment string) (string, error)
	ListCalendar(ctx context.Context, start, end time.Time) ([]CalendarEvent, error)
	SearchDocs(ctx context.Context, query string) ([]Doc, error)
	ReadDoc(ctx context.Context, id string) (Doc, error)
}

// Client is a small HTTP helper with bounded retries. It retries only
// transport errors and 429/503 responses, never a response the platform
// already answered (so an approval is never executed twice by retrying).
type Client struct {
	HTTP *http.Client
}

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) DoJSON(ctx context.Context, method, url string, headers map[string]string, body any, out any) error {
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		var r io.Reader
		if raw != nil {
			r = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, r)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
			continue
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode == 429 || resp.StatusCode == 503 {
			lastErr = fmt.Errorf("platform returned %d", resp.StatusCode)
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("platform returned %d: %s", resp.StatusCode, truncate(string(data), 500))
		}
		if out != nil {
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("decode platform response: %w", err)
			}
		}
		return nil
	}
	return fmt.Errorf("platform request failed after retries: %w", lastErr)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// MapStatus normalizes platform status words to pending/approved/rejected.
func MapStatus(s string) string {
	switch s {
	case "PENDING", "pending", "RUNNING", "PROCESSING":
		return "pending"
	case "APPROVED", "approved", "AGREE", "agree":
		return "approved"
	case "REJECTED", "rejected", "REFUSE", "refuse":
		return "rejected"
	default:
		return "unknown"
	}
}
