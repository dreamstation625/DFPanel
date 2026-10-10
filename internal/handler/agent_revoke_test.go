package handler

import (
	"dfpanel/internal/agenthub"
	"dfpanel/internal/database"
	"dfpanel/internal/model"
	"dfpanel/internal/proto"
	"errors"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestResetTokenClosesLiveConnectionAndRejectsOldCredentials(t *testing.T) {
	securityDB(t)
	hub := agenthub.New()
	agent := model.Agent{Name: "owner", NodeKey: "key", Secret: "secret", Roles: "frpc"}
	database.DB.Create(&agent)
	r := ginRouter()
	h := NewAgentHandler(hub)
	manage := NewAgentManageHandler(hub)
	r.GET("/api/agent/ws", h.WS)
	r.GET("/api/agent/configs", h.ManagedConfigs)
	r.POST("/api/agents/:id/token/reset", manage.ResetToken)
	server := httptest.NewServer(r)
	defer server.Close()
	req, _ := http.NewRequest("GET", server.URL+"/api/agent/ws", nil)
	proto.AuthorizeRequest(req, agent.Secret, agent.NodeKey, "0123456789abcdef0123456789abcdef", "11111111111111111111111111111111", agent.Roles, nil)
	ws, _, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http://", "ws://", 1)+"/api/agent/ws", req.Header)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	deadline := time.Now().Add(time.Second)
	for !hub.Online(agent.ID) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !hub.Online(agent.ID) {
		t.Fatal("连接未接入")
	}
	done := make(chan error, 1)
	go func() {
		_, err := hub.Send(agent.ID, proto.Envelope{Type: proto.MsgCommand, ID: "waiting"}, time.Minute)
		done <- err
	}()
	var env proto.Envelope
	ws.SetReadDeadline(time.Now().Add(time.Second))
	if err := ws.ReadJSON(&env); err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(server.URL+"/api/agents/"+strconv.Itoa(int(agent.ID))+"/token/reset", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 || hub.Online(agent.ID) {
		t.Fatal("重置未断开旧连接")
	}
	select {
	case err := <-done:
		if !errors.Is(err, agenthub.ErrOffline) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("撤销后在途调用未被唤醒")
	}
	req, _ = http.NewRequest("GET", server.URL+"/api/agent/configs", nil)
	proto.AuthorizeRequest(req, agent.Secret, agent.NodeKey, "0123456789abcdef0123456789abcdef", "11111111111111111111111111111111", agent.Roles, nil)
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("旧凭据仍可用")
	}
}
