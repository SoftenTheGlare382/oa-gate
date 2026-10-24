package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// FeishuAdapter talks to Feishu Open Platform (open.feishu.cn).
// Message send and approval decide are the Phase 1 path; calendar and
// document tools are read-only and need their optional config knobs.
// Approval endpoints follow Feishu Approval v4; if Feishu changes them,
// only this file needs to change.
type FeishuAdapter struct {
	AppID      string
	AppSecret  string
	BaseURL    string
	UserID     string
	CalendarID string
	client     *Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

func NewFeishu(appID, appSecret, baseURL, userID, calendarID string) *FeishuAdapter {
	if baseURL == "" {
		baseURL = "https://open.feishu.cn"
	}
	return &FeishuAdapter{AppID: appID, AppSecret: appSecret, BaseURL: baseURL, UserID: userID, CalendarID: calendarID, client: NewClient()}
}

func (a *FeishuAdapter) Name() string { return "feishu" }

func (a *FeishuAdapter) tokenFor(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.token != "" && time.Now().Before(a.tokenExp) {
		return a.token, nil
	}
	var resp struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	err := a.client.DoJSON(ctx, "POST", a.BaseURL+"/open-apis/auth/v3/tenant_access_token/internal",
		nil, map[string]string{"app_id": a.AppID, "app_secret": a.AppSecret}, &resp)
	if err != nil {
		return "", err
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("feishu token failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	a.token = resp.TenantAccessToken
	exp := resp.Expire - 60
	if exp < 60 {
		exp = 60
	}
	a.tokenExp = time.Now().Add(time.Duration(exp) * time.Second)
	return a.token, nil
}

func (a *FeishuAdapter) auth(ctx context.Context) (map[string]string, error) {
	tok, err := a.tokenFor(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]string{"Authorization": "Bearer " + tok}, nil
}

func (a *FeishuAdapter) SendMessage(ctx context.Context, target, text string) (string, error) {
	h, err := a.auth(ctx)
	if err != nil {
		return "", err
	}
	// Targets starting with oc_ are group chats, otherwise user ids.
	receiveType := "user_id"
	if len(target) >= 3 && target[:3] == "oc_" {
		receiveType = "chat_id"
	}
	content, _ := json.Marshal(map[string]string{"text": text})
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			MessageID string `json:"message_id"`
		} `json:"data"`
	}
	err = a.client.DoJSON(ctx, "POST",
		a.BaseURL+"/open-apis/im/v1/messages?receive_id_type="+receiveType, h,
		map[string]string{"receive_id": target, "msg_type": "text", "content": string(content)}, &resp)
	if err != nil {
		return "", err
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("feishu send failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return resp.Data.MessageID, nil
}

func (a *FeishuAdapter) ListApprovals(ctx context.Context, status string) ([]Approval, error) {
	if a.UserID == "" {
		return nil, NotImplementedf("feishu approval list needs feishu.user_id in config (the user whose tasks are queried)")
	}
	h, err := a.auth(ctx)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			TaskList []struct {
				TaskID   string `json:"task_id"`
				Status   string `json:"status"`
				Instance struct {
					Code  string `json:"instance_code"`
					Title string `json:"title"`
				} `json:"instance"`
			} `json:"task_list"`
		} `json:"data"`
	}
	body := map[string]any{"user_id": a.UserID, "page_size": 50}
	if status != "" {
		body["task_status"] = status
	}
	err = a.client.DoJSON(ctx, "POST", a.BaseURL+"/open-apis/approval/v4/tasks/query", h, body, &resp)
	if err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("feishu approval list failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	out := make([]Approval, 0, len(resp.Data.TaskList))
	for _, t := range resp.Data.TaskList {
		title := t.Instance.Title
		if title == "" {
			title = t.Instance.Code
		}
		out = append(out, Approval{ID: t.TaskID, Platform: "feishu", Title: title, Status: MapStatus(t.Status)})
	}
	return out, nil
}

func (a *FeishuAdapter) GetApproval(ctx context.Context, id string) (Approval, error) {
	// Feishu has no single-task read in Approval v4 that fits every tenant,
	// so get is implemented as list-and-find. It stays correct (and slow
	// but safe) rather than guessing another endpoint.
	list, err := a.ListApprovals(ctx, "")
	if err != nil {
		return Approval{}, err
	}
	for _, ap := range list {
		if ap.ID == id {
			return ap, nil
		}
	}
	return Approval{}, fmt.Errorf("feishu approval %s not found in task list", id)
}

func (a *FeishuAdapter) DecideApproval(ctx context.Context, id, decision, comment string) (string, error) {
	if a.UserID == "" {
		return "", NotImplementedf("feishu approval decide needs feishu.user_id in config")
	}
	h, err := a.auth(ctx)
	if err != nil {
		return "", err
	}
	path := "/open-apis/approval/v4/tasks/approve"
	if decision == "reject" {
		path = "/open-apis/approval/v4/tasks/reject"
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	err = a.client.DoJSON(ctx, "POST", a.BaseURL+path, h,
		map[string]string{"user_id": a.UserID, "task_id": id, "comment": comment}, &resp)
	if err != nil {
		return "", err
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("feishu approval decide failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return "feishu:" + id + ":" + decision, nil
}

func (a *FeishuAdapter) ListCalendar(ctx context.Context, start, end time.Time) ([]CalendarEvent, error) {
	if a.CalendarID == "" {
		return nil, NotImplementedf("feishu calendar list needs feishu.calendar_id in config")
	}
	h, err := a.auth(ctx)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Items []struct {
				EventID   string `json:"event_id"`
				Summary   string `json:"summary"`
				StartTime struct {
					Timestamp string `json:"timestamp"`
				} `json:"start_time"`
				EndTime struct {
					Timestamp string `json:"timestamp"`
				} `json:"end_time"`
			} `json:"items"`
		} `json:"data"`
	}
	url := fmt.Sprintf("%s/open-apis/calendar/v4/calendars/%s/events?page_size=50&time_min=%d&time_max=%d",
		a.BaseURL, a.CalendarID, start.Unix(), end.Unix())
	err = a.client.DoJSON(ctx, "GET", url, h, nil, &resp)
	if err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("feishu calendar list failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	out := make([]CalendarEvent, 0, len(resp.Data.Items))
	for _, e := range resp.Data.Items {
		ev := CalendarEvent{ID: e.EventID, Platform: "feishu", Title: e.Summary}
		if ts, err := time.Parse(time.RFC3339, e.StartTime.Timestamp); err == nil {
			ev.StartTime = ts
		}
		if ts, err := time.Parse(time.RFC3339, e.EndTime.Timestamp); err == nil {
			ev.EndTime = ts
		}
		out = append(out, ev)
	}
	return out, nil
}

func (a *FeishuAdapter) SearchDocs(ctx context.Context, query string) ([]Doc, error) {
	h, err := a.auth(ctx)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Entities []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"entities"`
		} `json:"data"`
	}
	err = a.client.DoJSON(ctx, "POST", a.BaseURL+"/open-apis/docx/v1/documents/search", h,
		map[string]string{"search_key": query}, &resp)
	if err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("feishu doc search failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	out := make([]Doc, 0, len(resp.Data.Entities))
	for _, d := range resp.Data.Entities {
		out = append(out, Doc{ID: d.ID, Platform: "feishu", Title: d.Title})
	}
	return out, nil
}

func (a *FeishuAdapter) ReadDoc(ctx context.Context, id string) (Doc, error) {
	h, err := a.auth(ctx)
	if err != nil {
		return Doc{}, err
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Document struct {
				DocumentID string `json:"document_id"`
				Title      string `json:"title"`
			} `json:"document"`
		} `json:"data"`
	}
	err = a.client.DoJSON(ctx, "GET", a.BaseURL+"/open-apis/docx/v1/documents/"+id, h, nil, &resp)
	if err != nil {
		return Doc{}, err
	}
	if resp.Code != 0 {
		return Doc{}, fmt.Errorf("feishu doc read failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return Doc{ID: resp.Data.Document.DocumentID, Platform: "feishu", Title: resp.Data.Document.Title}, nil
}
