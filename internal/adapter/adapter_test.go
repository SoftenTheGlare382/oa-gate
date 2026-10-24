package adapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFeishuMessageWithMockServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			w.Write([]byte(`{"code":0,"tenant_access_token":"tok","expire":7200}`))
		case "/open-apis/im/v1/messages":
			w.Write([]byte(`{"code":0,"data":{"message_id":"om-1"}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	a := NewFeishu("app", "secret", srv.URL, "", "")
	id, err := a.SendMessage(context.Background(), "user-1", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if id != "om-1" {
		t.Fatalf("got %q", id)
	}
}

func TestDingTalkAndWeComMessageWithMockServers(t *testing.T) {
	ding := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gettoken":
			w.Write([]byte(`{"errcode":0,"access_token":"tok","expires_in":7200}`))
		case "/topapi/message/corpconversation/asyncsend_v2":
			w.Write([]byte(`{"errcode":0,"task_id":42}`))
		default:
			t.Errorf("dingtalk unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer ding.Close()
	d := NewDingTalk("k", "s", "agent", "", ding.URL)
	if _, err := d.SendMessage(context.Background(), "u1", "hi"); err != nil {
		t.Fatal(err)
	}

	we := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			w.Write([]byte(`{"errcode":0,"access_token":"tok","expires_in":7200}`))
		case "/cgi-bin/message/send":
			w.Write([]byte(`{"errcode":0,"msgid":"m1"}`))
		default:
			t.Errorf("wecom unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer we.Close()
	wc := NewWeCom("corp", "agent", "secret", we.URL)
	id, err := wc.SendMessage(context.Background(), "u1", "hi")
	if err != nil {
		t.Fatal(err)
	}
	if id != "m1" {
		t.Fatalf("got %q", id)
	}
}

func TestDecideNotImplementedIsClear(t *testing.T) {
	d := NewDingTalk("k", "s", "agent", "", "https://example.invalid")
	if _, err := d.DecideApproval(context.Background(), "x", "approve", ""); err == nil {
		t.Fatal("dingtalk decide must return a clear not-implemented error, not a success")
	}
}
