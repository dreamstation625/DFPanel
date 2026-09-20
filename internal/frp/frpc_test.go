package frp

import (
	"strings"
	"testing"
)

func exampleFrpc() *FrpcConfig {
	loginFailExit := true
	tcpMux := true
	tlsEnable := false
	disableCustomTLSFirstByte := true

	return &FrpcConfig{
		ClientCommonConfig: ClientCommonConfig{
			Auth: AuthClientConfig{
				Method:           "token",
				Token:            "secret-token",
				AdditionalScopes: []string{"HeartBeats", "NewWorkConns"},
			},
			User:              "alice",
			ClientID:          "node-win-01",
			ServerAddr:        "frp.example.com",
			ServerPort:        7000,
			NatHoleSTUNServer: "stun.easyvoip.com:3478",
			DNSServer:         "8.8.8.8",
			LoginFailExit:     &loginFailExit,
			Start:             []string{"ssh", "web"},
			Log: &LogConfig{
				To:                "/data/frpc.log",
				Level:             "info",
				MaxDays:           3,
				DisablePrintColor: true,
			},
			WebServer: &WebServerConfig{
				Addr:        "127.0.0.1",
				Port:        7400,
				User:        "admin",
				Password:    "admin",
				PprofEnable: true,
			},
			Transport: &ClientTransportConfig{
				Protocol:                "tcp",
				DialServerTimeout:       10,
				DialServerKeepAlive:     60,
				ConnectServerLocalIP:    "0.0.0.0",
				ProxyURL:                "http://127.0.0.1:8087",
				PoolCount:               1,
				TCPMux:                  &tcpMux,
				TCPMuxKeepaliveInterval: 20,
				HeartbeatInterval:       30,
				HeartbeatTimeout:        90,
				TLS: &TLSClientConfig{
					Enable:                    &tlsEnable,
					DisableCustomTLSFirstByte: &disableCustomTLSFirstByte,
					TLSConfig: TLSConfig{
						TrustedCaFile: "/etc/frp/ca.crt",
						ServerName:    "frp.example.com",
					},
				},
			},
			UDPPacketSize:      1500,
			IncludeConfigFiles: []string{"./confd/*.ini"},
			Store:              &StoreConfig{Path: "/var/lib/frpc/storage"},
			FeatureGates:       map[string]bool{"TCPMux": true},
			Metadatas:          map[string]string{"os": "windows"},
		},
		Proxies: []map[string]any{
			{"name": "ssh", "type": "tcp", "localIP": "127.0.0.1", "localPort": 22, "remotePort": 6000},
		},
	}
}

func TestBuildFrpcJSON(t *testing.T) {
	content, err := BuildFrpcJSON(exampleFrpc())
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}

	out := decode(t, content)

	if out["serverAddr"] != "frp.example.com" || out["serverPort"] != num(7000) {
		t.Errorf("服务端地址解析错误: %v:%v", out["serverAddr"], out["serverPort"])
	}
	if out["user"] != "alice" {
		t.Errorf("user 期望 alice，实际 %v", out["user"])
	}
	if out["clientID"] != "node-win-01" {
		t.Errorf("clientID 解析错误: %v", out["clientID"])
	}
	if out["natHoleStunServer"] != "stun.easyvoip.com:3478" {
		t.Errorf("natHoleStunServer 解析错误: %v", out["natHoleStunServer"])
	}
	if out["dnsServer"] != "8.8.8.8" {
		t.Errorf("dnsServer 解析错误: %v", out["dnsServer"])
	}
	if out["loginFailExit"] != true {
		t.Errorf("loginFailExit 期望 true，实际 %v", out["loginFailExit"])
	}
	start := out["start"].([]any)
	if len(start) != 2 || start[0] != "ssh" || start[1] != "web" {
		t.Errorf("start 解析错误: %v", start)
	}
	if out["udpPacketSize"] != num(1500) {
		t.Errorf("udpPacketSize 解析错误: %v", out["udpPacketSize"])
	}
	includes := out["includes"].([]any)
	if len(includes) != 1 || includes[0] != "./confd/*.ini" {
		t.Errorf("includes 解析错误: %v", includes)
	}
	if out["featureGates"].(map[string]any)["TCPMux"] != true {
		t.Errorf("featureGates 解析错误: %v", out["featureGates"])
	}
	if out["metadatas"].(map[string]any)["os"] != "windows" {
		t.Errorf("metadatas 解析错误: %v", out["metadatas"])
	}

	auth := out["auth"].(map[string]any)
	if auth["method"] != "token" || auth["token"] != "secret-token" {
		t.Errorf("auth 解析错误: %v", auth)
	}
	scopes := auth["additionalScopes"].([]any)
	if len(scopes) != 2 {
		t.Errorf("additionalScopes 解析错误: %v", scopes)
	}

	transport := out["transport"].(map[string]any)
	if transport["protocol"] != "tcp" || transport["poolCount"] != num(1) {
		t.Errorf("transport 解析错误: %v", transport)
	}
	if transport["dialServerTimeout"] != num(10) || transport["dialServerKeepalive"] != num(60) {
		t.Errorf("dialServer 参数解析错误: %v", transport)
	}
	if transport["tcpMux"] != true || transport["tcpMuxKeepaliveInterval"] != num(20) {
		t.Errorf("tcpMux 参数解析错误: %v", transport)
	}
	if transport["heartbeatInterval"] != num(30) || transport["heartbeatTimeout"] != num(90) {
		t.Errorf("heartbeat 参数解析错误: %v", transport)
	}
	tls := transport["tls"].(map[string]any)
	if tls["enable"] != false {
		t.Errorf("tls.enable 关闭时应显式输出 false，实际 %v", tls["enable"])
	}
	if tls["disableCustomTLSFirstByte"] != true {
		t.Errorf("disableCustomTLSFirstByte 解析错误: %v", tls["disableCustomTLSFirstByte"])
	}
	if tls["trustedCaFile"] != "/etc/frp/ca.crt" || tls["serverName"] != "frp.example.com" {
		t.Errorf("tls 证书字段解析错误: %v", tls)
	}

	ws := out["webServer"].(map[string]any)
	if ws["addr"] != "127.0.0.1" || ws["port"] != num(7400) {
		t.Errorf("webServer 解析错误: %v", ws)
	}
	if ws["pprofEnable"] != true {
		t.Errorf("webServer.pprofEnable 解析错误: %v", ws["pprofEnable"])
	}

	log := out["log"].(map[string]any)
	if log["to"] != "/data/frpc.log" || log["level"] != "info" || log["maxDays"] != num(3) {
		t.Errorf("log 解析错误: %v", log)
	}
	if log["disablePrintColor"] != true {
		t.Errorf("log.disablePrintColor 解析错误: %v", log)
	}

	store := out["store"].(map[string]any)
	if store["path"] != "/var/lib/frpc/storage" {
		t.Errorf("store.path 解析错误: %v", store)
	}

	proxies := out["proxies"].([]any)
	if len(proxies) != 1 {
		t.Fatalf("proxies 应有 1 项，实际 %d", len(proxies))
	}
	proxy := proxies[0].(map[string]any)
	if proxy["name"] != "ssh" || proxy["type"] != "tcp" || proxy["remotePort"] != num(6000) {
		t.Errorf("proxy 解析错误: %v", proxy)
	}

	if _, ok := out["visitors"]; ok {
		t.Error("未配置 visitors 时不应输出该字段")
	}
}

func TestBuildFrpcJSONMinimal(t *testing.T) {
	cfg := &FrpcConfig{
		ClientCommonConfig: ClientCommonConfig{
			ServerAddr: "127.0.0.1",
			ServerPort: 7000,
			Auth:       AuthClientConfig{Token: "abc"},
		},
	}
	content, err := BuildFrpcJSON(cfg)
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	out := decode(t, content)
	if out["serverPort"] != num(7000) {
		t.Errorf("serverPort 解析错误: %v", out["serverPort"])
	}
	if _, ok := out["webServer"]; ok {
		t.Error("未配置时不应输出 webServer")
	}
	if _, ok := out["virtualNet"]; ok {
		t.Error("未配置时不应输出 virtualNet")
	}
	if !strings.Contains(content, "\n") {
		t.Error("产物应为缩进的多行 JSON")
	}
}
