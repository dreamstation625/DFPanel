package frp

import (
	"encoding/json"
	"testing"

	"dfpanel/internal/model"
)

func TestCleanServerAddr(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://1.2.3.4:7226", "1.2.3.4"},
		{"https://frp.example.com", "frp.example.com"},
		{"https://frp.example.com/", "frp.example.com"},
		{"  http://1.2.3.4/path?a=1  ", "1.2.3.4"},
		{"1.2.3.4", "1.2.3.4"},
		{"frp.example.com:7000", "frp.example.com"},
		{"", ""},
		{"[::1]:7000", "[::1]:7000"}, // IPv6 字面量不动
	}
	for _, c := range cases {
		if got := cleanServerAddr(c.in); got != c.want {
			t.Errorf("cleanServerAddr(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

// 面板对外地址是 http://host:port，拿它兜底生成 serverAddr 时不能把协议和端口带上
func TestFallbackAddrStripsScheme(t *testing.T) {
	out, err := BuildFrpcFromNode(&model.Node{Name: "n"}, nil, nil, nil, "http://1.2.3.4:7226")
	if err != nil {
		t.Fatalf("生成 frpc 配置失败：%v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("解析 frpc 配置失败：%v", err)
	}
	if m["serverAddr"] != "1.2.3.4" {
		t.Errorf("serverAddr = %v，期望 1.2.3.4", m["serverAddr"])
	}
	if m["serverPort"] != float64(7000) {
		t.Errorf("serverPort = %v，期望 7000", m["serverPort"])
	}
}

// 服务端配置里填的公网地址带协议时同样要清理
func TestServerPublicAddrStripsScheme(t *testing.T) {
	server := &model.FrpsServer{Name: "s", PublicAddr: "http://frp.example.com:7000", BindPort: 7001}
	out, err := BuildFrpcFromNode(&model.Node{Name: "n"}, server, nil, nil, "http://1.2.3.4:7226")
	if err != nil {
		t.Fatalf("生成 frpc 配置失败：%v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("解析 frpc 配置失败：%v", err)
	}
	if m["serverAddr"] != "frp.example.com" {
		t.Errorf("serverAddr = %v，期望 frp.example.com", m["serverAddr"])
	}
	if m["serverPort"] != float64(7001) {
		t.Errorf("serverPort = %v，期望 7001（取 frps 的 bindPort）", m["serverPort"])
	}
}

// 节点没填 authToken 时要回退用 frps 的 auth.token —— 面板表单里写的就是「留空使用 frps 的 auth.token」
func TestNodeTokenFallsBackToServer(t *testing.T) {
	server := &model.FrpsServer{Name: "s", BindPort: 7001, AuthToken: "srv-token"}
	out, err := BuildFrpcFromNode(&model.Node{Name: "n"}, server, nil, nil, "")
	if err != nil {
		t.Fatalf("生成 frpc 配置失败：%v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("解析 frpc 配置失败：%v", err)
	}
	auth, _ := m["auth"].(map[string]any)
	if auth == nil || auth["token"] != "srv-token" {
		t.Errorf("auth = %v，期望回退到 frps 的 token", m["auth"])
	}

	// 节点自己填了 token 时以节点为准
	out2, _ := BuildFrpcFromNode(&model.Node{Name: "n", AuthToken: "node-token"}, server, nil, nil, "")
	var m2 map[string]any
	_ = json.Unmarshal([]byte(out2), &m2)
	auth2, _ := m2["auth"].(map[string]any)
	if auth2 == nil || auth2["token"] != "node-token" {
		t.Errorf("auth = %v，期望用节点自己的 token", m2["auth"])
	}
}
