package frp

import (
	"encoding/json"
	"strings"
	"testing"

	"dfpanel/internal/model"
)

// TestBuildProxiesNestsTransportAndLoadBalancer 锁定 proxy 的嵌套层级。
//
// frp 的 ProxyBaseConfig 里 transport / loadBalancer 都是嵌套结构，扁平写法会被判为未知字段：
// v0.71.0 起 --strict_config 默认为 true，frpc verify 会直接报
// `unknown field "useEncryption"`，配置根本下发不下去。
// 实测（frpc v0.71.0）：扁平写法 verify 失败，嵌套写法通过。
func TestBuildProxiesNestsTransportAndLoadBalancer(t *testing.T) {
	got := buildProxies([]model.Proxy{{
		Name:           "ssh",
		Type:           "tcp",
		LocalIP:        "127.0.0.1",
		LocalPort:      22,
		RemotePort:     6001,
		UseEncryption:  true,
		UseCompression: true,
		GroupName:      "g1",
		Enabled:        true,
	}})
	if len(got) != 1 {
		t.Fatalf("期望 1 条代理，得到 %d", len(got))
	}
	p := got[0]

	// 顶层不得再出现这三个键
	for _, bad := range []string{"useEncryption", "useCompression", "group", "groupKey"} {
		if _, ok := p[bad]; ok {
			t.Errorf("顶层不应出现 %q（会被 frp 判为未知字段）：%v", bad, p)
		}
	}

	tr, ok := p["transport"].(map[string]any)
	if !ok {
		t.Fatalf("transport 缺失或类型不对：%v", p["transport"])
	}
	if tr["useEncryption"] != true || tr["useCompression"] != true {
		t.Errorf("transport 下应带 useEncryption/useCompression：%v", tr)
	}

	lb, ok := p["loadBalancer"].(map[string]any)
	if !ok {
		t.Fatalf("loadBalancer 缺失或类型不对：%v", p["loadBalancer"])
	}
	if lb["group"] != "g1" || lb["groupKey"] != "g1" {
		t.Errorf("loadBalancer 下的 group/groupKey 不正确：%v", lb)
	}
}

// TestBuildProxiesOmitsEmptyNestedSections 未开启时不写入空对象，避免污染生成的配置
func TestBuildProxiesOmitsEmptyNestedSections(t *testing.T) {
	got := buildProxies([]model.Proxy{{
		Name: "web", Type: "http", LocalIP: "127.0.0.1", LocalPort: 80,
		Subdomain: "web", Enabled: true,
	}})
	if len(got) != 1 {
		t.Fatalf("期望 1 条代理，得到 %d", len(got))
	}
	p := got[0]
	if _, ok := p["transport"]; ok {
		t.Errorf("未启用加密压缩时不应写 transport：%v", p)
	}
	if _, ok := p["loadBalancer"]; ok {
		t.Errorf("未填分组名时不应写 loadBalancer：%v", p)
	}
}

// TestBuildProxiesSkipsDisabledAndSerializes 禁用隧道不生成；序列化结果中不应出现扁平旧键
func TestBuildProxiesSkipsDisabledAndSerializes(t *testing.T) {
	proxies := []model.Proxy{
		{Name: "on", Type: "tcp", LocalIP: "127.0.0.1", LocalPort: 22, RemotePort: 6001, Enabled: true, UseEncryption: true},
		{Name: "off", Type: "tcp", LocalIP: "127.0.0.1", LocalPort: 23, RemotePort: 6002, Enabled: false},
	}
	got := buildProxies(proxies)
	if len(got) != 1 || got[0]["name"] != "on" {
		t.Fatalf("应只保留启用的隧道，得到 %v", got)
	}

	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, `"useEncryption":true,"useCompression"`) {
		t.Errorf("序列化结果里出现了扁平键：%s", s)
	}
	if !strings.Contains(s, `"transport":{"useEncryption":true}`) {
		t.Errorf("序列化结果缺少嵌套的 transport.useEncryption：%s", s)
	}
}
