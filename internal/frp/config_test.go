package frp

import (
	"encoding/json"
	"testing"

	"dfpanel/internal/model"
)

func boolPtr(v bool) *bool { return &v }

func testServer() *model.FrpsServer {
	return &model.FrpsServer{
		Name:                            "default",
		BindAddr:                        "0.0.0.0",
		BindPort:                        7000,
		KCPBindPort:                     7000,
		QUICBindPort:                    7001,
		ProxyBindAddr:                   "0.0.0.0",
		TCPMuxHTTPConnectPort:           1337,
		SubdomainHost:                   "example.com",
		Custom404Page:                   "/etc/frp/404.html",
		VhostHTTPPort:                   80,
		VhostHTTPTimeout:                60,
		VhostHTTPSPort:                  443,
		AuthMethod:                      "token",
		AuthToken:                       "secret-token",
		DashboardEnabled:                boolPtr(true),
		DashboardPort:                   7500,
		DashboardUser:                   "admin",
		DashboardPwd:                    "admin",
		DashboardAssetsDir:              "/etc/frp/assets",
		DashboardPprofEnable:            true,
		TcpMux:                          boolPtr(true),
		TcpMuxKeepaliveInterval:         30,
		TcpKeepalive:                    7200,
		MaxPoolCount:                    5,
		MaxPortsPerClient:               5,
		UserConnTimeout:                 10,
		UDPPacketSize:                   1500,
		NatHoleAnalysisDataReserveHours: 168,
		DetailedErrorsToClient:          boolPtr(true),
		AllowPorts:                      "20000-30000,30001",
		LogLevel:                        "info",
		LogMaxDays:                      7,
	}
}

func num(v float64) any { return v }

func decode(t *testing.T, content string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		t.Fatalf("产物不是合法 JSON: %v", err)
	}
	return out
}

func TestBuildFrpsJSON(t *testing.T) {
	s := testServer()

	content, err := BuildFrpsJSON(s, "")
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}

	out := decode(t, content)

	// 监听相关
	if out["bindAddr"] != "0.0.0.0" {
		t.Errorf("bindAddr 期望 0.0.0.0，实际 %v", out["bindAddr"])
	}
	if out["bindPort"] != num(7000) {
		t.Errorf("bindPort 期望 7000，实际 %v", out["bindPort"])
	}
	if out["kcpBindPort"] != num(7000) {
		t.Errorf("kcpBindPort 期望 7000，实际 %v", out["kcpBindPort"])
	}
	if out["quicBindPort"] != num(7001) {
		t.Errorf("quicBindPort 期望 7001，实际 %v", out["quicBindPort"])
	}
	if out["proxyBindAddr"] != "0.0.0.0" {
		t.Errorf("proxyBindAddr 期望 0.0.0.0，实际 %v", out["proxyBindAddr"])
	}

	// 虚拟主机
	if out["vhostHTTPPort"] != num(80) {
		t.Errorf("vhostHTTPPort 期望 80，实际 %v", out["vhostHTTPPort"])
	}
	if out["vhostHTTPTimeout"] != num(60) {
		t.Errorf("vhostHTTPTimeout 期望 60，实际 %v", out["vhostHTTPTimeout"])
	}
	if out["vhostHTTPSPort"] != num(443) {
		t.Errorf("vhostHTTPSPort 期望 443，实际 %v", out["vhostHTTPSPort"])
	}
	if out["subDomainHost"] != "example.com" {
		t.Errorf("subDomainHost 期望 example.com，实际 %v", out["subDomainHost"])
	}
	if out["custom404Page"] != "/etc/frp/404.html" {
		t.Errorf("custom404Page 解析异常: %v", out["custom404Page"])
	}
	// 拼写必须是 tcpmuxHTTPConnectPort（frp 对未知字段会报错）
	if out["tcpmuxHTTPConnectPort"] != num(1337) {
		t.Errorf("tcpmuxHTTPConnectPort 期望 1337，实际 %v", out["tcpmuxHTTPConnectPort"])
	}
	if _, ok := out["tcpMuxHTTPConnectPort"]; ok {
		t.Error("不应出现拼写错误的 tcpMuxHTTPConnectPort 字段")
	}

	// 鉴权
	auth := out["auth"].(map[string]any)
	if auth["method"] != "token" {
		t.Errorf("auth.method 期望 token，实际 %v", auth["method"])
	}
	if auth["token"] != "secret-token" {
		t.Errorf("auth.token 期望 secret-token，实际 %v", auth["token"])
	}

	// Dashboard
	ws := out["webServer"].(map[string]any)
	if ws["addr"] != "0.0.0.0" {
		t.Errorf("webServer.addr 期望 0.0.0.0，实际 %v", ws["addr"])
	}
	if ws["port"] != num(7500) {
		t.Errorf("webServer.port 期望 7500，实际 %v", ws["port"])
	}
	if ws["user"] != "admin" || ws["password"] != "admin" {
		t.Errorf("webServer 凭据不正确: %v / %v", ws["user"], ws["password"])
	}
	if ws["assetsDir"] != "/etc/frp/assets" {
		t.Errorf("webServer.assetsDir 期望 /etc/frp/assets，实际 %v", ws["assetsDir"])
	}
	if ws["pprofEnable"] != true {
		t.Errorf("webServer.pprofEnable 期望 true，实际 %v", ws["pprofEnable"])
	}
	if _, ok := ws["tls"]; ok {
		t.Errorf("未配置证书时不应输出空的 webServer.tls: %v", ws["tls"])
	}

	// 传输层
	transport := out["transport"].(map[string]any)
	if transport["tcpMux"] != true {
		t.Errorf("transport.tcpMux 期望 true，实际 %v", transport["tcpMux"])
	}
	if transport["tcpMuxKeepaliveInterval"] != num(30) {
		t.Errorf("transport.tcpMuxKeepaliveInterval 期望 30，实际 %v", transport["tcpMuxKeepaliveInterval"])
	}
	if transport["tcpKeepalive"] != num(7200) {
		t.Errorf("transport.tcpKeepalive 期望 7200，实际 %v", transport["tcpKeepalive"])
	}
	if transport["maxPoolCount"] != num(5) {
		t.Errorf("transport.maxPoolCount 期望 5，实际 %v", transport["maxPoolCount"])
	}
	if _, ok := transport["tls"]; ok {
		t.Errorf("未启用强制 TLS 时不应输出空的 tls 对象: %v", transport["tls"])
	}
	if _, ok := transport["quic"]; ok {
		t.Errorf("未配置 QUIC 参数时不应输出空的 quic 对象: %v", transport["quic"])
	}

	// 顶层其他项
	if out["maxPortsPerClient"] != num(5) {
		t.Errorf("maxPortsPerClient 期望 5，实际 %v", out["maxPortsPerClient"])
	}
	if out["userConnTimeout"] != num(10) {
		t.Errorf("userConnTimeout 期望 10，实际 %v", out["userConnTimeout"])
	}
	if out["udpPacketSize"] != num(1500) {
		t.Errorf("udpPacketSize 期望 1500，实际 %v", out["udpPacketSize"])
	}
	if out["natholeAnalysisDataReserveHours"] != num(168) {
		t.Errorf("natholeAnalysisDataReserveHours 期望 168，实际 %v", out["natholeAnalysisDataReserveHours"])
	}
	if out["detailedErrorsToClient"] != true {
		t.Errorf("detailedErrorsToClient 期望 true，实际 %v", out["detailedErrorsToClient"])
	}
	if _, ok := out["enablePrometheus"]; ok {
		t.Errorf("未启用 Prometheus 时不应输出 enablePrometheus 字段")
	}
	if _, ok := out["sshTunnelGateway"]; ok {
		t.Errorf("未配置 SSH 隧道网关时不应输出该字段")
	}

	// 端口池
	allowPorts := out["allowPorts"].([]any)
	if len(allowPorts) != 2 {
		t.Fatalf("allowPorts 应有 2 项，实际 %d", len(allowPorts))
	}
	rng := allowPorts[0].(map[string]any)
	if rng["start"] != num(20000) || rng["end"] != num(30000) {
		t.Errorf("区间解析错误: %v", rng)
	}
	single := allowPorts[1].(map[string]any)
	if single["single"] != num(30001) {
		t.Errorf("单一端口应使用 single 字段: %v", single)
	}

	// 日志
	log := out["log"].(map[string]any)
	if log["level"] != "info" {
		t.Errorf("log.level 期望 info，实际 %v", log["level"])
	}
	if log["maxDays"] != num(7) {
		t.Errorf("log.maxDays 期望 7，实际 %v", log["maxDays"])
	}
	if _, ok := log["to"]; ok {
		t.Errorf("日志路径为空时不该输出 log.to")
	}
}

func TestBuildFrpsJSONTransportTLSAndQUIC(t *testing.T) {
	s := testServer()
	s.TLSForce = true
	s.TLSCertFile = "/etc/frp/server.crt"
	s.TLSKeyFile = "/etc/frp/server.key"
	s.TLSTrustedCaFile = "/etc/frp/ca.crt"
	s.TLSServerName = "frp.example.com"
	s.QuicKeepalivePeriod = 10
	s.QuicMaxIdleTimeout = 30
	s.QuicMaxIncomingStreams = 100000
	s.HeartbeatTimeout = 90

	out := decode(t, mustBuild(t, s))

	transport := out["transport"].(map[string]any)
	if transport["heartbeatTimeout"] != num(90) {
		t.Errorf("heartbeatTimeout 期望 90，实际 %v", transport["heartbeatTimeout"])
	}
	tls := transport["tls"].(map[string]any)
	if tls["force"] != true {
		t.Errorf("tls.force 期望 true，实际 %v", tls["force"])
	}
	if tls["certFile"] != "/etc/frp/server.crt" || tls["keyFile"] != "/etc/frp/server.key" {
		t.Errorf("tls 证书路径不正确: %v", tls)
	}
	if tls["trustedCaFile"] != "/etc/frp/ca.crt" || tls["serverName"] != "frp.example.com" {
		t.Errorf("tls 可信 CA / serverName 不正确: %v", tls)
	}
	quic := transport["quic"].(map[string]any)
	if quic["keepalivePeriod"] != num(10) || quic["maxIdleTimeout"] != num(30) || quic["maxIncomingStreams"] != num(100000) {
		t.Errorf("quic 参数不正确: %v", quic)
	}
}

func TestBuildFrpsJSONAuthOIDC(t *testing.T) {
	s := testServer()
	s.AuthMethod = "oidc"
	s.AuthAdditionalScopes = "HeartBeats, NewWorkConns"
	s.AuthOIDCIssuer = "https://example.com:8443/dex"
	s.AuthOIDCAudience = "https://example.com"
	s.AuthOIDCSkipExpiryCheck = true
	s.AuthOIDCSkipIssuerCheck = true

	out := decode(t, mustBuild(t, s))

	auth := out["auth"].(map[string]any)
	if auth["method"] != "oidc" {
		t.Errorf("auth.method 期望 oidc，实际 %v", auth["method"])
	}
	scopes := auth["additionalScopes"].([]any)
	if len(scopes) != 2 || scopes[0] != "HeartBeats" || scopes[1] != "NewWorkConns" {
		t.Errorf("additionalScopes 解析错误: %v", scopes)
	}
	oidc := auth["oidc"].(map[string]any)
	if oidc["issuer"] != "https://example.com:8443/dex" {
		t.Errorf("oidc.issuer 解析错误: %v", oidc["issuer"])
	}
	if oidc["audience"] != "https://example.com" {
		t.Errorf("oidc.audience 解析错误: %v", oidc["audience"])
	}
	if oidc["skipExpiryCheck"] != true || oidc["skipIssuerCheck"] != true {
		t.Errorf("oidc 校验开关解析错误: %v", oidc)
	}
}

func TestBuildFrpsJSONSSHTunnelGateway(t *testing.T) {
	s := testServer()
	s.SSHGatewayBindPort = 2201
	s.SSHGatewayPrivateKeyFile = "/etc/frp/ssh_host_ed25519_key"
	s.SSHGatewayAutoGenPrivateKeyPath = "/var/lib/frp/ssh_tunnel_gateway"
	s.SSHGatewayAuthorizedKeysFile = "/etc/frp/authorized_keys"

	out := decode(t, mustBuild(t, s))
	gw := out["sshTunnelGateway"].(map[string]any)
	if gw["bindPort"] != num(2201) {
		t.Errorf("sshTunnelGateway.bindPort 期望 2201，实际 %v", gw["bindPort"])
	}
	if gw["privateKeyFile"] != "/etc/frp/ssh_host_ed25519_key" {
		t.Errorf("privateKeyFile 不正确: %v", gw["privateKeyFile"])
	}
	if gw["autoGenPrivateKeyPath"] != "/var/lib/frp/ssh_tunnel_gateway" {
		t.Errorf("autoGenPrivateKeyPath 不正确: %v", gw["autoGenPrivateKeyPath"])
	}
	if gw["authorizedKeysFile"] != "/etc/frp/authorized_keys" {
		t.Errorf("authorizedKeysFile 不正确: %v", gw["authorizedKeysFile"])
	}
}

func TestBuildFrpsJSONDashboardTLS(t *testing.T) {
	s := testServer()
	s.DashboardAddr = "127.0.0.1"
	s.DashboardTLSCertFile = "/etc/frp/dashboard.crt"
	s.DashboardTLSKeyFile = "/etc/frp/dashboard.key"
	s.LogDisablePrintColor = true

	out := decode(t, mustBuild(t, s))
	ws := out["webServer"].(map[string]any)
	if ws["addr"] != "127.0.0.1" {
		t.Errorf("webServer.addr 应支持自定义，实际 %v", ws["addr"])
	}
	tls := ws["tls"].(map[string]any)
	if tls["certFile"] != "/etc/frp/dashboard.crt" || tls["keyFile"] != "/etc/frp/dashboard.key" {
		t.Errorf("webServer.tls 证书不正确: %v", tls)
	}
	log := out["log"].(map[string]any)
	if log["disablePrintColor"] != true {
		t.Errorf("log.disablePrintColor 期望 true，实际 %v", log["disablePrintColor"])
	}
}

func TestBuildFrpsJSONLogPath(t *testing.T) {
	s := testServer()
	s.LogLevel = ""
	s.LogMaxDays = 0

	content, err := BuildFrpsJSON(s, "/data/frps-1.log")
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	out := decode(t, content)

	log := out["log"].(map[string]any)
	if log["to"] != "/data/frps-1.log" {
		t.Errorf("log.to 期望 /data/frps-1.log，实际 %v", log["to"])
	}
	if log["level"] != "info" {
		t.Errorf("日志级别为空时应回填默认值 info，实际 %v", log["level"])
	}
	if _, ok := log["maxDays"]; ok {
		t.Errorf("maxDays 为 0 时不该输出该字段，应交给 frps 默认值")
	}
}

func TestBuildFrpsJSONTransportDefaults(t *testing.T) {
	s := testServer()
	s.TcpMux = boolPtr(false)
	s.TLSForce = true

	content, err := BuildFrpsJSON(s, "")
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}

	out := decode(t, content)
	transport := out["transport"].(map[string]any)
	// frp 默认 tcpMux = true，关闭时必须显式输出 false
	if transport["tcpMux"] != false {
		t.Errorf("tcpMux 关闭时应显式输出 false，实际 %v", transport["tcpMux"])
	}
	tls := transport["tls"].(map[string]any)
	if tls["force"] != true {
		t.Errorf("tls.force 期望 true，实际 %v", tls["force"])
	}
}

func TestBuildFrpsJSONDashboardUnspecified(t *testing.T) {
	s := testServer()
	s.DashboardEnabled = nil

	out := decode(t, mustBuild(t, s))
	if _, ok := out["webServer"]; !ok {
		t.Error("未指定 DashboardEnabled 时应按默认启用输出 webServer")
	}
}

func TestBuildFrpsJSONDashboardDisabled(t *testing.T) {
	s := testServer()
	s.DashboardEnabled = boolPtr(false)

	out := decode(t, mustBuild(t, s))
	if ws, ok := out["webServer"]; ok {
		t.Errorf("Dashboard 关闭时不该输出 webServer 段，实际: %v", ws)
	}
}

func TestBuildFrpsJSONDashboardZeroPort(t *testing.T) {
	s := testServer()
	s.DashboardEnabled = boolPtr(true)
	s.DashboardPort = 0

	out := decode(t, mustBuild(t, s))
	if ws, ok := out["webServer"]; ok {
		t.Errorf("端口为 0 时不该输出 webServer 段，实际: %v", ws)
	}
}

func TestBuildFrpsJSONExtraOverride(t *testing.T) {
	s := testServer()
	s.ExtraJSON = `{"bindPort": 7100, "transport": {"maxPoolCount": 50}}`

	out := decode(t, mustBuild(t, s))
	if out["bindPort"] != num(7100) {
		t.Errorf("额外配置应覆盖 bindPort，实际 %v", out["bindPort"])
	}
	transport := out["transport"].(map[string]any)
	if transport["maxPoolCount"] != num(50) {
		t.Errorf("额外配置应整体覆盖 transport，实际 %v", transport["maxPoolCount"])
	}
}

func TestBuildFrpsJSONExtraInvalid(t *testing.T) {
	s := testServer()
	s.ExtraJSON = `{"bindPort": 7100` // 缺少右括号

	if _, err := BuildFrpsJSON(s, ""); err == nil {
		t.Error("额外配置非法时应返回错误")
	}
}

func TestParseAllowPorts(t *testing.T) {
	res := ParseAllowPorts(" 1-3 , 5 ,100")
	if len(res) != 3 {
		t.Fatalf("期望解析出 3 项，实际 %d", len(res))
	}
	if res[0].Start != 1 || res[0].End != 3 {
		t.Errorf("区间解析错误: %+v", res[0])
	}
	if res[1].Single != 5 {
		t.Errorf("单一端口解析错误: %+v", res[1])
	}
	if res[2].Single != 100 {
		t.Errorf("单一端口解析错误: %+v", res[2])
	}
	if ParseAllowPorts("") != nil {
		t.Error("空输入应返回 nil")
	}
	// 反序区间自动纠正
	res = ParseAllowPorts("300-100")
	if res[0].Start != 100 || res[0].End != 300 {
		t.Errorf("反序区间应自动纠正: %+v", res[0])
	}
}

func mustBuild(t *testing.T, s *model.FrpsServer) string {
	t.Helper()
	content, err := BuildFrpsJSON(s, "")
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	return content
}
