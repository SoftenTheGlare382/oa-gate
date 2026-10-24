package adapter

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// WeComAdapter talks to WeCom (企业微信) Open Platform.
// Message send and approval read are implemented. Approval decide is
// intentionally NOT guessed (see DingTalkAdapter for the same reasoning):
// the gateway returns a clear error instead of risking a wrong execution.
type WeComAdapter struct {
	CorpID  string
	AgentID string
	Secret  string
	BaseURL string
	client  *Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

func NewWeCom(corpID, agentID, secret, baseURL string) *WeComAdapter {
	if baseURL == "" {
		baseURL = "https://qyapi.weixin.qq.com"
	}
	return &WeComAdapter{CorpID: corpID, AgentID: agentID, Secret: secret, BaseURL: baseURL, client: NewClient()}
}

func (a *WeComAdapter) Name() string { return "wecom" }

func (a *WeComAdapter) tokenFor(ctx context.Context) (string, error) {
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
	url := fmt.Sprintf("%s/cgi-bin/gettoken?corpid=%s&corpsecret=%s", a.BaseURL, a.CorpID, a.Secret)
	if err := a.client.DoJSON(ctx, "GET", url, nil, nil, &resp); err != nil {
		return "", err
	}
	if resp.ErrCode != 0 {
		return "", fmt.Errorf("wecom token failed: errcode=%d errmsg=%s", resp.ErrCode, resp.ErrMsg)
	}
	a.token = resp.AccessToken
	exp := resp.ExpiresIn - 60
	if exp < 60 {
		exp = 60
	}
	a.tokenExp = time.Now().Add(time.Duration(exp) * time.Second)
	return a.token, nil
}

func (a *WeComAdapter) SendMessage(ctx context.Context, target, text string) (string, error) {
	tok, err := a.tokenFor(ctx)
	if err != nil {
		return "", err
	}
	var resp struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		MsgID   string `json:"msgid"`
	}
	body := map[string]any{
		"touser":  target,
		"agentid": a.AgentID,
		"msgtype": "text",
		"text":    map[string]string{"content": text},
	}
	url := fmt.Sprintf("%s/cgi-bin/message/send?access_token=%s", a.BaseURL, tok)
	if err := a.client.DoJSON(ctx, "POST", url, nil, body, &resp); err != nil {
		return "", err
	}
	if resp.ErrCode != 0 {
		return "", fmt.Errorf("wecom send failed: errcode=%d errmsg=%s", resp.ErrCode, resp.ErrMsg)
	}
	return resp.MsgID, nil
}

func (a *WeComAdapter) ListApprovals(ctx context.Context, status string) ([]Approval, error) {
	tok, err := a.tokenFor(ctx)
	if err != nil {
		return nil, err
	}
	var listResp struct {
		ErrCode  int      `json:"errcode"`
		ErrMsg   string   `json:"errmsg"`
		SpNoList []string `json:"sp_no_list"`
	}
	body := map[string]any{
		"starttime": time.Now().Add(-30 * 24 * time.Hour).Unix(),
		"endtime":   time.Now().Unix(),
	}
	url := fmt.Sprintf("%s/cgi-bin/oa/getapprovaldata?access_token=%s", a.BaseURL, tok)
	if err := a.client.DoJSON(ctx, "POST", url, nil, body, &listResp); err != nil {
		return nil, err
	}
	if listResp.ErrCode != 0 {
		return nil, fmt.Errorf("wecom approval list failed: errcode=%d errmsg=%s", listResp.ErrCode, listResp.ErrMsg)
	}
	out := make([]Approval, 0, len(listResp.SpNoList))
	for _, id := range listResp.SpNoList {
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

func (a *WeComAdapter) GetApproval(ctx context.Context, id string) (Approval, error) {
	tok, err := a.tokenFor(ctx)
	if err != nil {
		return Approval{}, err
	}
	var resp struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		SpNo    string `json:"sp_no"`
		Info    struct {
			SpName  string `json:"sp_name"`
			Applyer struct {
				Userid string `json:"userid"`
			} `json:"applyer"`
			SpStatus int `json:"sp_status"`
		} `json:"info"`
	}
	url := fmt.Sprintf("%s/cgi-bin/oa/getapprovalinfo?access_token=%s", a.BaseURL, tok)
	if err := a.client.DoJSON(ctx, "POST", url, nil, map[string]string{"sp_no": id}, &resp); err != nil {
		return Approval{}, err
	}
	if resp.ErrCode != 0 {
		return Approval{}, fmt.Errorf("wecom approval get failed: errcode=%d errmsg=%s", resp.ErrCode, resp.ErrMsg)
	}
	status := "unknown"
	switch resp.Info.SpStatus {
	case 1:
		status = "pending"
	case 2:
		status = "approved"
	case 3:
		status = "rejected"
	}
	return Approval{ID: resp.SpNo, Platform: "wecom", Title: resp.Info.SpName, Applicant: resp.Info.Applyer.Userid, Status: status}, nil
}

func (a *WeComAdapter) DecideApproval(ctx context.Context, id, decision, comment string) (string, error) {
	return "", NotImplementedf("wecom approval decide is not implemented in v0.1: oa-gate refuses rather than guessing an endpoint. Message send and approval read work; decide must be done in WeCom until a verified implementation is added")
}

func (a *WeComAdapter) ListCalendar(ctx context.Context, start, end time.Time) ([]CalendarEvent, error) {
	return nil, NotImplementedf("wecom calendar is not implemented in v0.1")
}

func (a *WeComAdapter) SearchDocs(ctx context.Context, query string) ([]Doc, error) {
	return nil, NotImplementedf("wecom docs are not implemented in v0.1")
}

func (a *WeComAdapter) ReadDoc(ctx context.Context, id string) (Doc, error) {
	return Doc{}, NotImplementedf("wecom docs are not implemented in v0.1")
}
