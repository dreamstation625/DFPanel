package frp

import (
	"encoding/json"
	"testing"

	"dfpanel/internal/model"
)

func parseBuilt(t *testing.T, proxies []model.Proxy, visitors []model.Visitor, node *model.Node) map[string]any {
	t.Helper()
	out, err := BuildFrpcFromNode(node, nil, proxies, visitors, "")
	if err != nil {
		t.Fatalf("BuildFrpcFromNode 失败：%v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("生成的配置不是合法 JSON：%v\n%s", err, out)
	}
	return m
}

func proxyByName(t *testing.T, m map[string]any, name string) map[string]any {
	t.Helper()
	list, _ := m["proxies"].([]any)
	for _, item := range list {
		if p, ok := item.(map[string]any); ok && p["name"] == name {
			return p
		}
	}
	t.Fatalf("未找到代理 %s", name)
	return nil
}

// TestProxyFieldsLandInCorrectTypeScope 校验「类型专属字段」不会串到别的类型上 ——
// 串了就是未知字段，严格模式下 frpc verify 直接报错。
func TestProxyFieldsLandInCorrectTypeScope(t *testing.T) {
	proxies := []model.Proxy{
		{Name: "h", Type: "http", LocalPort: 80, Enabled: true, CustomDomains: "a.com",
			HTTPUser: "u", HTTPPassword: "p", Locations: "/,/api", HostHeaderRewrite: "x", RouteByHTTPUser: "u"},
		{Name: "s", Type: "https", LocalPort: 443, Enabled: true, CustomDomains: "b.com",
			HTTPUser: "u", HTTPPassword: "p", Locations: "/x"},
		{Name: "m", Type: "tcpmux", LocalPort: 10701, Enabled: true, CustomDomains: "c",
			Multiplexer: "httpconnect", HTTPUser: "u"},
		{Name: "st", Type: "stcp", LocalPort: 22, Enabled: true, SecretKey: "k", AllowUsers: "a,b"},
		{Name: "xt", Type: "xtcp", LocalPort: 22, Enabled: true, SecretKey: "k", DisableAssistedAddrs: true},
		{Name: "t", Type: "tcp", LocalPort: 22, RemotePort: 6001, Enabled: true},
	}
	m := parseBuilt(t, proxies, nil, &model.Node{Name: "n", ServerAddr: "1.2.3.4", ServerPort: 7000})

	h := proxyByName(t, m, "h")
	if h["locations"] == nil || h["httpUser"] != "u" || h["hostHeaderRewrite"] != "x" || h["routeByHTTPUser"] != "u" {
		t.Errorf("http 应带 locations/httpUser/hostHeaderRewrite/routeByHTTPUser：%v", h)
	}
	s := proxyByName(t, m, "s")
	for _, bad := range []string{"httpUser", "httpPassword", "locations", "hostHeaderRewrite", "routeByHTTPUser", "multiplexer"} {
		if _, ok := s[bad]; ok {
			t.Errorf("https 不应带 %q（会成未知字段）：%v", bad, s)
		}
	}
	mx := proxyByName(t, m, "m")
	if mx["multiplexer"] != "httpconnect" || mx["httpUser"] != "u" {
		t.Errorf("tcpmux 应带 multiplexer/httpUser：%v", mx)
	}
	if _, ok := mx["hostHeaderRewrite"]; ok {
		t.Errorf("tcpmux 不应带 hostHeaderRewrite：%v", mx)
	}
	st := proxyByName(t, m, "st")
	if st["secretKey"] != "k" {
		t.Errorf("stcp 应带 secretKey：%v", st)
	}
	if _, ok := st["natTraversal"]; ok {
		t.Errorf("stcp 不应带 natTraversal（仅 xtcp 支持）：%v", st)
	}
	if users, ok := st["allowUsers"].([]any); !ok || len(users) != 2 {
		t.Errorf("stcp 的 allowUsers 应解析成数组：%v", st["allowUsers"])
	}
	xt := proxyByName(t, m, "xt")
	if nt, ok := xt["natTraversal"].(map[string]any); !ok || nt["disableAssistedAddrs"] != true {
		t.Errorf("xtcp 应带 natTraversal.disableAssistedAddrs：%v", xt)
	}
	tp := proxyByName(t, m, "t")
	if tp["remotePort"] == nil {
		t.Errorf("tcp 应带 remotePort：%v", tp)
	}
	for _, bad := range []string{"secretKey", "allowUsers", "multiplexer", "httpUser"} {
		if _, ok := tp[bad]; ok {
			t.Errorf("普通 tcp 不应带 %q：%v", bad, tp)
		}
	}
}

// TestPluginParamsParsedAsJSON 回归：插件参数是 JSON 对象，早期误用 key=value 解析器
// 导致参数被整段丢弃（官方 frpc 报 localPath is required）。
func TestPluginParamsParsedAsJSON(t *testing.T) {
	proxies := []model.Proxy{{
		Name: "socks", Type: "tcp", LocalPort: 0, RemotePort: 6005, Enabled: true,
		PluginType:   "socks5",
		PluginConfig: `{"username":"abc","password":"abc"}`,
	}}
	m := parseBuilt(t, proxies, nil, &model.Node{Name: "n", ServerAddr: "1.2.3.4", ServerPort: 7000})
	p := proxyByName(t, m, "socks")

	pl, ok := p["plugin"].(map[string]any)
	if !ok {
		t.Fatalf("plugin 缺失：%v", p)
	}
	if pl["type"] != "socks5" || pl["username"] != "abc" || pl["password"] != "abc" {
		t.Errorf("插件参数未正确展开：%v", pl)
	}
	if _, ok := p["localIP"]; ok {
		t.Errorf("配了插件时不应写 localIP/localPort：%v", p)
	}
}

// TestPluginTypeWinsOverConfigType 两处都写 type 时以 PluginType 为准
func TestPluginTypeWinsOverConfigType(t *testing.T) {
	proxies := []model.Proxy{{
		Name: "p", Type: "tcp", RemotePort: 6005, Enabled: true,
		PluginType: "socks5", PluginConfig: `{"type":"http_proxy","username":"a"}`,
	}}
	m := parseBuilt(t, proxies, nil, &model.Node{Name: "n", ServerAddr: "1.2.3.4", ServerPort: 7000})
	pl := proxyByName(t, m, "p")["plugin"].(map[string]any)
	if pl["type"] != "socks5" {
		t.Errorf("plugin.type 应以 PluginType 为准，得到 %v", pl["type"])
	}
}

func TestParseJSONObject(t *testing.T) {
	if v, err := ParseJSONObject(""); err != nil || v != nil {
		t.Errorf("空串应返回 nil, nil，得到 %v %v", v, err)
	}
	if v, err := ParseJSONObject(`{"a":1,"b":"x"}`); err != nil || v["a"].(float64) != 1 || v["b"] != "x" {
		t.Errorf("解析结果不对：%v %v", v, err)
	}
	if _, err := ParseJSONObject(`{"a":1`); err == nil {
		t.Error("非法 JSON 应报错")
	}
}

// TestVisitorsBuiltCorrectly 访问端：基础字段 + xtcp 专属字段 + 负数 bindPort + 停用跳过
func TestVisitorsBuiltCorrectly(t *testing.T) {
	visitors := []model.Visitor{
		{Name: "v1", Type: "stcp", ServerName: "svc", SecretKey: "k", BindAddr: "127.0.0.1", BindPort: -1, Enabled: true},
		{Name: "v2", Type: "xtcp", ServerName: "svc", ServerUser: "u1", SecretKey: "k",
			BindAddr: "127.0.0.1", BindPort: 9001, KeepTunnelOpen: true, MaxRetriesAnHour: 8,
			MinRetryInterval: 90, FallbackTo: "v1", FallbackTimeoutMs: 500,
			DisableAssistedAddrs: true, UseEncryption: true, Enabled: true},
		{Name: "off", Type: "stcp", ServerName: "svc", Enabled: false},
	}
	m := parseBuilt(t, nil, visitors, &model.Node{Name: "n", ServerAddr: "1.2.3.4", ServerPort: 7000})
	list, _ := m["visitors"].([]any)
	if len(list) != 2 {
		t.Fatalf("停用的访问端不应生成，得到 %d 条", len(list))
	}
	v1 := list[0].(map[string]any)
	if v1["bindPort"].(float64) != -1 {
		t.Errorf("负数 bindPort 应照实下发：%v", v1)
	}
	for _, bad := range []string{"keepTunnelOpen", "natTraversal", "protocol", "fallbackTo"} {
		if _, ok := v1[bad]; ok {
			t.Errorf("stcp 访问端不应带 %q（仅 xtcp）：%v", bad, v1)
		}
	}
	v2 := list[1].(map[string]any)
	if v2["keepTunnelOpen"] != true || v2["serverUser"] != "u1" || v2["fallbackTo"] != "v1" {
		t.Errorf("xtcp 访问端字段不完整：%v", v2)
	}
	if nt, ok := v2["natTraversal"].(map[string]any); !ok || nt["disableAssistedAddrs"] != true {
		t.Errorf("xtcp 访问端应带 natTraversal：%v", v2)
	}
	if tr, ok := v2["transport"].(map[string]any); !ok || tr["useEncryption"] != true {
		t.Errorf("访问端 transport 未生成：%v", v2)
	}
}

// TestClientTLSEnableAlwaysEmitted 回归：frp 自 v0.50 起 transport.tls.enable 默认为 true，
// 只在开启时才写的话，界面上「关掉 TLS」不会生效。
func TestClientTLSEnableAlwaysEmitted(t *testing.T) {
	for _, enable := range []bool{true, false} {
		m := parseBuilt(t, nil, nil, &model.Node{Name: "n", ServerAddr: "1.2.3.4", ServerPort: 7000, TLSEnable: enable})
		tr, ok := m["transport"].(map[string]any)
		if !ok {
			t.Fatalf("transport 必须存在：%v", m)
		}
		tls, ok := tr["tls"].(map[string]any)
		if !ok {
			t.Fatalf("transport.tls 必须存在：%v", tr)
		}
		if tls["enable"] != enable {
			t.Errorf("TLSEnable=%v 时应显式下发 enable=%v，实际 %v", enable, enable, tls["enable"])
		}
	}
}

// TestTokenSourceMutuallyExclusiveWithToken tokenSource 与 token 不能同时出现
func TestTokenSourceMutuallyExclusiveWithToken(t *testing.T) {
	m := parseBuilt(t, nil, nil, &model.Node{Name: "n", ServerAddr: "1.2.3.4", ServerPort: 7000,
		AuthToken: "should-be-ignored", AuthTokenSourceType: "file", AuthTokenSourcePath: "/etc/frp/token"})
	auth := m["auth"].(map[string]any)
	if _, ok := auth["token"]; ok {
		t.Errorf("配了 tokenSource 就不应再写 token：%v", auth)
	}
	ts, ok := auth["tokenSource"].(map[string]any)
	if !ok || ts["type"] != "file" {
		t.Fatalf("tokenSource 未生成：%v", auth)
	}
	if f, ok := ts["file"].(map[string]any); !ok || f["path"] != "/etc/frp/token" {
		t.Errorf("tokenSource.file.path 不对：%v", ts)
	}
}

func TestParseHTTPPlugins(t *testing.T) {
	if got, err := ParseHTTPPlugins(""); err != nil || got != nil {
		t.Errorf("空串应返回 nil,nil：%v %v", got, err)
	}
	got, err := ParseHTTPPlugins(`[{"name":"a","addr":"127.0.0.1:9000","ops":["Login"]}]`)
	if err != nil || len(got) != 1 {
		t.Fatalf("解析失败：%v %v", got, err)
	}
	if got[0].Path != "/handler" {
		t.Errorf("path 缺省应补 /handler，得到 %q", got[0].Path)
	}
	if _, err := ParseHTTPPlugins(`[{"addr":"x"}]`); err == nil {
		t.Error("缺少 name 应报错")
	}
	if _, err := ParseHTTPPlugins(`{"name":"a"}`); err == nil {
		t.Error("不是数组应报错")
	}
}

// TestParseKVAndScalars 元信息：key=value 行、值类型还原
func TestParseKVAndScalars(t *testing.T) {
	got := parseKV("a=1\nb=true\nc=hello\nprefix/key2=value2")
	if got["a"] != "1" || got["b"] != "true" || got["c"] != "hello" || got["prefix/key2"] != "value2" {
		t.Errorf("parseKV 结果不对：%v", got)
	}
	any := parseAnyKV("n=1\nf=true\ns=x")
	if any["n"].(json.Number).String() != "1" || any["f"] != true || any["s"] != "x" {
		t.Errorf("parseAnyKV 类型还原不对：%#v", any)
	}
	if got := parseKV("没有等号"); len(got) != 0 {
		t.Error("没有等号的行应被忽略")
	}
}

// TestHealthCheckOnlyWhenEnabled 未选类型时不生成 healthCheck
func TestHealthCheckOnlyWhenEnabled(t *testing.T) {
	m := parseBuilt(t, []model.Proxy{
		{Name: "off", Type: "tcp", RemotePort: 1, Enabled: true},
		{Name: "on", Type: "tcp", RemotePort: 2, Enabled: true, HealthCheckType: "tcp",
			HealthCheckIntervalSeconds: 10, HealthCheckMaxFailed: 3, HealthCheckTimeoutSeconds: 3},
	}, nil, &model.Node{Name: "n", ServerAddr: "1.2.3.4", ServerPort: 7000})

	if _, ok := proxyByName(t, m, "off")["healthCheck"]; ok {
		t.Error("未启用健康检查时不应生成 healthCheck")
	}
	hc, ok := proxyByName(t, m, "on")["healthCheck"].(map[string]any)
	if !ok || hc["type"] != "tcp" || hc["intervalSeconds"].(float64) != 10 {
		t.Errorf("healthCheck 生成不对：%v", hc)
	}
}

// TestMinimalNodeConfigStaysSmall 空字段不应写进配置（避免生成一堆无意义默认值）
func TestMinimalNodeConfigStaysSmall(t *testing.T) {
	m := parseBuilt(t, nil, nil, &model.Node{Name: "n", ServerAddr: "1.2.3.4", ServerPort: 7000, AuthToken: "t"})

	for _, bad := range []string{"poolCount", "dnsServer", "wireProtocol", "metadatas", "udpPacketSize",
		"clientID", "natHoleStunServer", "loginFailExit", "proxies", "visitors"} {
		if _, ok := m[bad]; ok {
			t.Errorf("空字段不应出现在配置里：%q", bad)
		}
	}
	tr, ok := m["transport"].(map[string]any)
	if !ok {
		t.Fatalf("transport 必须存在：%v", m)
	}
	// 只有 tls.enable 是必写的，其余 transport 子项留空
	if len(tr) != 1 {
		t.Errorf("未配置的 transport 子项不应出现，实际：%v", tr)
	}
}
