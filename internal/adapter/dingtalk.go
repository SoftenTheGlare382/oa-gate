package adapter

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// DingTalkAdapter talks to DingTalk Open Platform.
// Message send is implemented (work notification). Approval read is
// implemented when dingtalk.process_code is configured. Approval decide
// is intentionally NOT guessed: DingTalk's task-approval API varies by
// process type, so the gateway returns a clear error instead of risking
// a wrong or duplicate execution. See README for the status table.
type DingTalkAdapter struct {
	AppKey      string
	AppSecret   string
	AgentID     string
	ProcessCode string
	BaseURL     string
	client      *Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

func NewDingTalk(appKey, appSecret, agentID, processCode, baseURL string) *DingTalkAdapter {
	if baseURL == "" {
		baseURL = "https://oapi.dingtalk.com"
	}
	return &DingTalkAdapter{AppKey: appKey, AppSecret: appSecret, AgentID: agentID, ProcessCode: processCode, BaseURL: baseURL, client: NewClient()}
}

func (a *DingTalkAdapter) Name() string { return "dingtalk" }

func (a *DingTalkAdapter) tokenFor(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.token != "" && time.Now().Before(a.tokenExp) {
		return a.token, nil
	}
	var resp struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	url := fmt.Sprintf("%s/gettoken?appkey=%s&appsecret=%s", a.BaseURL, a.AppKey, a.AppSecret)
	if err := a.client.DoJSON(ctx, "GET", url, nil, nil, &resp); err != nil {
		return "", err
	}
	if resp.ErrCode != 0 {
		return "", fmt.Errorf("dingtalk token failed: errcode=%d errmsg=%s", resp.ErrCode, resp.ErrMsg)
	}
	a.token = resp.AccessToken
	exp := resp.ExpiresIn - 60
	if exp < 60 {
		exp = 60
	}
	a.tokenExp = time.Now().Add(time.Duration(exp) * time.Second)
	return a.token, nil
}

func (a *DingTalkAdapter) SendMessage(ctx context.Context, target, text string) (string, error) {
	tok, err := a.tokenFor(ctx)
	if err != nil {
		return "", err
	}
	var resp struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		TaskID  int64  `json:"task_id"`
	}
	body := map[string]any{
		"agent_id":    a.AgentID,
		"userid_list": target,
		"msg":         map[string]any{"msgtype": "text", "text": map[string]string{"content": text}},
	}
	url := fmt.Sprintf("%s/topapi/message/corpconversation/asyncsend_v2?access_token=%s", a.BaseURL, tok)
	if err := a.client.DoJSON(ctx, "POST", url, nil, body, &resp); err != nil {
		return "", err
	}
	if resp.ErrCode != 0 {
		return "", fmt.Errorf("dingtalk send failed: errcode=%d errmsg=%s", resp.ErrCode, resp.ErrMsg)
	}
	return fmt.Sprintf("dingtalk:%d", resp.TaskID), nil
}

func (a *DingTalkAdapter) ListApprovals(ctx context.Context, status string) ([]Approval, error) {
	if a.ProcessCode == "" {
		return nil, NotImplementedf("dingtalk approval list needs dingtalk.process_code in config")
	}
	tok, err := a.tokenFor(ctx)
	if err != nil {
		return nil, err
	}
	var listResp struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		Result  struct {
			List []string `json:"list"`
		} `json:"result"`
	}
	body := map[string]any{
		"process_code": a.ProcessCode,
		"start_time":   time.Now().Add(-30 * 24 * time.Hour).UnixMilli(),
		"end_time":     time.Now().UnixMilli(),
		"size":         20,
	}
	url := fmt.Sprintf("%s/topapi/processinstance/listids?access_token=%s", a.BaseURL, tok)
	if err := a.client.DoJSON(ctx, "POST", url, nil, body, &listResp); err != nil {
		return nil, err
	}
	if listResp.ErrCode != 0 {
		return nil, fmt.Errorf("dingtalk approval list failed: errcode=%d errmsg=%s", listResp.ErrCode, listResp.ErrMsg)
	}
	out := make([]Approval, 0, len(listResp.Result.List))
	for _, id := range listResp.Result.List {
		ap, err := a.GetApproval(ctx, id)
		if err != nil {
			continue
		}
		if status == "" || ap.Status == status {
			out = append(out, ap)
		}
	}
	return out, nil
}

func (a *DingTalkAdapter) GetApproval(ctx context.Context, id string) (Approval, error) {
	tok, err := a.tokenFor(ctx)
	if err != nil {
		return Approval{}, err
	}
	var resp struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		Result  struct {
			Title      string `json:"title"`
			Status     string `json:"status"`
			Originator struct {
				Userid string `json:"userid"`
			} `json:"originator_userid"`
			CreateTime string `json:"create_time"`
		} `json:"process_instance"`
	}
	url := fmt.Sprintf("%s/topapi/processinstance/get?access_token=%s", a.BaseURL, tok)
	if err := a.client.DoJSON(ctx, "POST", url, nil, map[string]string{"process_instance_id": id}, &resp); err != nil {
		return Approval{}, err
	}
	if resp.ErrCode != 0 {
		return Approval{}, fmt.Errorf("dingtalk approval get failed: errcode=%d errmsg=%s", resp.ErrCode, resp.ErrMsg)
	}
	ap := Approval{ID: id, Platform: "dingtalk", Title: resp.Result.Title, Applicant: resp.Result.Originator.Userid, Status: MapStatus(resp.Result.Status)}
	if t, err := time.Parse(time.RFC3339, resp.Result.CreateTime); err == nil {
		ap.CreatedAt = t
	}
	return ap, nil
}

func (a *DingTalkAdapter) DecideApproval(ctx context.Context, id, decision, comment string) (string, error) {
	return "", NotImplementedf("dingtalk approval decide is not implemented in v0.1: DingTalk task approval varies by process type, so oa-gate refuses rather than guessing an endpoint. Message send and approval read work; decide must be done in DingTalk until a process-specific implementation is added")
}

func (a *DingTalkAdapter) ListCalendar(ctx context.Context, start, end time.Time) ([]CalendarEvent, error) {
	return nil, NotImplementedf("dingtalk calendar is not implemented in v0.1")
}

func (a *DingTalkAdapter) SearchDocs(ctx context.Context, query string) ([]Doc, error) {
	return nil, NotImplementedf("dingtalk docs are not implemented in v0.1")
}

func (a *DingTalkAdapter) ReadDoc(ctx context.Context, id string) (Doc, error) {
	return Doc{}, NotImplementedf("dingtalk docs are not implemented in v0.1")
}
