package frp

import (
	"strings"

	"dfpanel/internal/model"
)

// BuildFrpcFromNode 依据节点、其关联的 frps 与隧道列表生成 frpc.json
// fallbackAddr 为面板对外地址，在节点与服务端都未指定地址时兜底
func BuildFrpcFromNode(node *model.Node, server *model.FrpsServer, proxies []model.Proxy, fallbackAddr string) (string, error) {
	serverAddr := node.ServerAddr
	serverPort := node.ServerPort
	token := node.AuthToken

	if server != nil {
		if serverAddr == "" {
			serverAddr = server.PublicAddr
		}
		if serverPort == 0 {
			serverPort = server.BindPort
		}
		if token == "" {
			token = server.AuthToken
		}
	}
	if serverAddr == "" {
		serverAddr = fallbackAddr
	}
	if serverPort == 0 {
		serverPort = 7000
	}

	cfg := &FrpcConfig{}
	cfg.ServerAddr = serverAddr
	cfg.ServerPort = serverPort
	cfg.Auth = AuthClientConfig{Method: "token", Token: token}
	cfg.User = node.User
	// Agent 侧统一捕获 stdout 写日志文件，便于健康检查与面板查看日志
	cfg.Log = &LogConfig{To: "console", Level: "info", MaxDays: 3}

	if node.TLSEnable {
		enable := true
		cfg.Transport = &ClientTransportConfig{TLS: &TLSClientConfig{Enable: &enable}}
	}
	if node.Protocol != "" && node.Protocol != "tcp" {
		if cfg.Transport == nil {
			cfg.Transport = &ClientTransportConfig{}
		}
		cfg.Transport.Protocol = node.Protocol
	}

	cfg.Proxies = buildProxies(proxies)
	return BuildFrpcJSON(cfg)
}

// buildProxies 把隧道记录转换成 frpc 的 proxies 数组
func buildProxies(proxies []model.Proxy) []map[string]any {
	if len(proxies) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(proxies))
	for _, p := range proxies {
		if !p.Enabled {
			continue
		}
		m := map[string]any{
			"name":      p.Name,
			"type":      p.Type,
			"localIP":   defaultString(p.LocalIP, "127.0.0.1"),
			"localPort": p.LocalPort,
		}
		switch strings.ToLower(p.Type) {
		case "tcp", "udp":
			if p.RemotePort > 0 {
				m["remotePort"] = p.RemotePort
			}
		case "http", "https":
			if p.Subdomain != "" {
				m["subdomain"] = p.Subdomain
			}
			if p.CustomDomains != "" {
				m["customDomains"] = splitList(p.CustomDomains)
			}
		}
		if p.UseEncryption {
			m["useEncryption"] = true
		}
		if p.UseCompression {
			m["useCompression"] = true
		}
		if p.GroupName != "" {
			m["group"] = p.GroupName
			m["groupKey"] = p.GroupName
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func splitList(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' })
	res := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			res = append(res, p)
		}
	}
	return res
}

func defaultString(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
