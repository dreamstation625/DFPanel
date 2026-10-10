package agent

import (
	"dfpanel/internal/proto"
	"encoding/json"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueuedCommandIsExecutedWhileWebSocketStaysConnected(t *testing.T) {
	var pulls atomic.Int32
	reported := make(chan struct{}, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if err := proto.VerifyRequest(r, "secret", b); err != nil {
			http.Error(w, err.Error(), 401)
			return
		}
		switch r.URL.Path {
		case "/api/agent/ws":
			ws, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer ws.Close()
			ws.WriteJSON(proto.Envelope{Type: proto.MsgPing})
			for {
				var env proto.Envelope
				if ws.ReadJSON(&env) != nil {
					return
				}
			}
		case "/api/agent/commands":
			cmds := []proto.CommandData{}
			if pulls.Add(1) == 1 {
				cmds = append(cmds, proto.CommandData{CommandID: 1, Type: proto.CmdUnassign, TargetType: proto.TargetNode, TargetID: 1})
			}
			json.NewEncoder(w).Encode(map[string]any{"commands": cmds})
		case "/api/agent/report":
			var result struct {
				CommandID uint `json:"commandId"`
				proto.ResultData
			}
			json.Unmarshal(b, &result)
			if result.CommandID != 1 || !result.OK {
				t.Errorf("错误结果：%+v", result)
			}
			select {
			case reported <- struct{}{}:
			default:
			}
			io.WriteString(w, `{}`)
		default:
			io.WriteString(w, `{}`)
		}
	}))
	defer server.Close()
	cfg := &Config{DataDir: t.TempDir(), PanelURL: server.URL, NodeKey: "key", Secret: "secret", Roles: "frpc", Runtime: "process", InstanceID: "0123456789abcdef0123456789abcdef", HostID: "11111111111111111111111111111111"}
	a := New(cfg)
	ws, err := a.client.DialWS()
	if err != nil {
		t.Fatal(err)
	}
	wsDone := make(chan struct{})
	go func() { a.serveWS(ws); close(wsDone) }()
	deadline := time.Now().Add(time.Second)
	for !a.wsConnected.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	stop := make(chan struct{})
	pollDone := make(chan struct{})
	go func() { a.pollLoop(stop); close(pollDone) }()
	t.Cleanup(func() { close(stop); ws.Close(); <-wsDone; <-pollDone })
	select {
	case <-reported:
	case <-time.After(5 * time.Second):
		t.Fatal("长连接阻止了积压命令执行")
	}
	if !a.wsConnected.Load() {
		t.Fatal("命令依赖断开长连接才执行")
	}
}
