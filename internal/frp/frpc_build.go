package frp

import (
	"encoding/json"
	"errors"
	"strings"

	"dfpanel/internal/model"
)

// BuildFrpcFromNode 依据节点、其关联的 frps 与隧道列表生成 frpc.json
// fallbackAddr 为面板对外地址，在节点与服务端都未指定地址时兜底
func BuildFrpcFromNode(node *model.Node, server *model.FrpsServer, proxies []model.Proxy, visitors []model.Visitor, fallbackAddr string) (string, error) {
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
	cfg.Auth = buildClientAuth(node)
	cfg.User = node.User
	cfg.ClientID = node.ClientID
	// Agent 侧统一捕获 stdout 写日志文件，便于健康检查与面板查看日志
	cfg.Log = &LogConfig{To: "console", Level: "info", MaxDays: 3}

	cfg.NatHoleSTUNServer = node.NatHoleSTUNServer
	cfg.DNSServer = node.DNSServer
	cfg.LoginFailExit = node.LoginFailExit
	cfg.UDPPacketSize = node.UDPPacketSize
	cfg.Metadatas = parseKV(node.Metadatas)

	cfg.Transport = buildClientTransport(node)

	cfg.Proxies = buildProxies(proxies)
	cfg.Visitors = buildVisitors(visitors)
	return BuildFrpcJSON(cfg)
}

// buildClientAuth 客户端鉴权：token 与 tokenSource 互斥；oidc 走单独分支
func buildClientAuth(node *model.Node) AuthClientConfig {
	out := AuthClientConfig{Method: defaultString(node.AuthMethod, "token")}

	if out.Method == "oidc" {
		oidc := &AuthOIDCClientConfig{
			ClientID:         node.AuthOIDCClientID,
			ClientSecret:     node.AuthOIDCClientSecret,
			Audience:         node.AuthOIDCAudience,
			Scope:            node.AuthOIDCScope,
			TokenEndpointURL: node.AuthOIDCTokenEndpointURL,
		}
		if kv := parseKV(node.AuthOIDCAdditionalParams); len(kv) > 0 {
			oidc.AdditionalEndpointParams = kv
		}
		out.OIDC = oidc
		return out
	}

	if node.AuthTokenSourcePath != "" {
		// tokenSource 与 token 互斥，配了文件来源就不再写 token
		out.TokenSource = &ValueSource{
			Type: defaultString(node.AuthTokenSourceType, "file"),
			File: &FileSource{Path: node.AuthTokenSourcePath},
		}
		return out
	}
	out.Token = node.AuthToken
	return out
}

// buildClientTransport 客户端网络层配置。
//
// transport.tls.enable 一律显式下发：frp 自 v0.50.0 起该值默认为 true，若只在开启时才写，
// 界面上把 TLS 关掉就不会生效（配置里缺省 = frp 用默认 true）。这里显式写 false 才能真正关掉。
func buildClientTransport(node *model.Node) *ClientTransportConfig {
	t := &ClientTransportConfig{}

	if node.Protocol != "" && node.Protocol != "tcp" {
		t.Protocol = node.Protocol
	}
	t.WireProtocol = node.WireProtocol
	t.DialServerTimeout = node.DialServerTimeout
	t.DialServerKeepAlive = node.DialServerKeepAlive
	t.ConnectServerLocalIP = node.ConnectServerLocalIP
	t.ProxyURL = node.ProxyURL
	t.PoolCount = node.PoolCount
	t.TCPMux = node.TCPMux
	t.TCPMuxKeepaliveInterval = node.TCPMuxKeepaliveInterval
	t.HeartbeatInterval = node.HeartbeatInterval
	t.HeartbeatTimeout = node.HeartbeatTimeout

	t.TLS = &TLSClientConfig{
		Enable:                    &node.TLSEnable,
		DisableCustomTLSFirstByte: node.DisableCustomTLSFirstByte,
		TLSConfig: TLSConfig{
			CertFile:      node.TLSCertFile,
			KeyFile:       node.TLSKeyFile,
			TrustedCaFile: node.TLSTrustedCaFile,
			ServerName:    node.TLSServerName,
		},
	}
	return t
}

// buildProxies 把隧道记录转换成 frpc 的 proxies 数组。
//
// ⚠️ 字段必须落到 frp 规定的层级与类型下（见 pkg/config/v1/proxy.go）：
//   - transport.*：useEncryption / useCompression / proxyProtocolVersion / bandwidthLimit
//   - loadBalancer.*：group / groupKey
//   - healthCheck.* / annotations / metadatas / plugin：ProxyBaseConfig，所有类型通用
//   - http 专属：locations / httpUser / httpPassword / hostHeaderRewrite / routeByHTTPUser
//   - tcpmux 专属：httpUser / httpPassword / routeByHTTPUser / multiplexer
//   - stcp / sudp / xtcp 专属：secretKey / allowUsers
//   - xtcp 专属：natTraversal
//
// 发错类型或写错层级都会变成「未知字段」：严格模式下 frpc verify 直接报错。
func buildProxies(proxies []model.Proxy) []map[string]any {
	if len(proxies) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(proxies))
	for _, p := range proxies {
		if !p.Enabled {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(p.Type))
		m := map[string]any{
			"name": p.Name,
			"type": typ,
		}

		// 配了插件时 localIP/localPort 不生效，写进去反而误导
		if p.PluginType == "" {
			m["localIP"] = defaultString(p.LocalIP, "127.0.0.1")
			m["localPort"] = p.LocalPort
		}

		switch typ {
		case "tcp", "udp":
			if p.RemotePort > 0 {
				m["remotePort"] = p.RemotePort
			}
		case "http", "https", "tcpmux":
			if p.Subdomain != "" {
				m["subdomain"] = p.Subdomain
			}
			if domains := splitList(p.CustomDomains); len(domains) > 0 {
				m["customDomains"] = domains
			}
		}
		if typ == "http" || typ == "https" {
			// 只有 http 支持这些
			if typ == "http" {
				if locs := splitList(p.Locations); len(locs) > 0 {
					m["locations"] = locs
				}
				if p.HostHeaderRewrite != "" {
					m["hostHeaderRewrite"] = p.HostHeaderRewrite
				}
			}
		}
		if typ == "http" || typ == "tcpmux" {
			if p.HTTPUser != "" {
				m["httpUser"] = p.HTTPUser
			}
			if p.HTTPPassword != "" {
				m["httpPassword"] = p.HTTPPassword
			}
			if p.RouteByHTTPUser != "" {
				m["routeByHTTPUser"] = p.RouteByHTTPUser
			}
		}
		if typ == "tcpmux" {
			m["multiplexer"] = defaultString(p.Multiplexer, "httpconnect")
		}
		if p.IsP2P() {
			if p.SecretKey != "" {
				m["secretKey"] = p.SecretKey
			}
			if users := splitList(p.AllowUsers); len(users) > 0 {
				m["allowUsers"] = users
			}
			// natTraversal 仅 xtcp 支持
			if typ == "xtcp" && p.DisableAssistedAddrs {
				m["natTraversal"] = map[string]any{"disableAssistedAddrs": true}
			}
		}

		// ---- transport：加密 / 压缩 / PROXY protocol / 限速 ----
		transport := map[string]any{}
		if p.UseEncryption {
			transport["useEncryption"] = true
		}
		if p.UseCompression {
			transport["useCompression"] = true
		}
		if v := strings.TrimSpace(p.ProxyProtocolVersion); v != "" {
			transport["proxyProtocolVersion"] = v
		}
		if v := strings.TrimSpace(p.BandwidthLimit); v != "" {
			transport["bandwidthLimit"] = v
		}
		if v := strings.TrimSpace(p.BandwidthLimitMode); v != "" {
			transport["bandwidthLimitMode"] = v
		}
		if len(transport) > 0 {
			m["transport"] = transport
		}

		// ---- loadBalancer：同名代理之间的负载均衡 ----
		if p.GroupName != "" {
			m["loadBalancer"] = map[string]any{
				"group":    p.GroupName,
				"groupKey": p.GroupName,
			}
		}

		// ---- 健康检查 ----
		if hc := buildHealthCheck(p); hc != nil {
			m["healthCheck"] = hc
		}

		if kv := parseKV(p.Annotations); len(kv) > 0 {
			m["annotations"] = kv
		}
		if kv := parseKV(p.Metadatas); len(kv) > 0 {
			m["metadatas"] = kv
		}

		if plugin := buildPlugin(p); plugin != nil {
			m["plugin"] = plugin
		}

		out = append(out, m)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// buildVisitors 把访问端记录转换成 frpc 的 visitors 数组。
//
// 字段归属（pkg/config/v1/visitor.go）：
//   - VisitorBaseConfig（所有类型）：name / type / enabled / transport(useEncryption,useCompression)
//     / secretKey / serverUser / serverName / bindAddr / bindPort / plugin
//   - 仅 xtcp：protocol / keepTunnelOpen / maxRetriesAnHour / minRetryInterval
//     / fallbackTo / fallbackTimeoutMs / natTraversal
func buildVisitors(visitors []model.Visitor) []map[string]any {
	if len(visitors) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(visitors))
	for _, v := range visitors {
		if !v.Enabled {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(v.Type))
		m := map[string]any{
			"name":       v.Name,
			"type":       typ,
			"serverName": v.ServerName,
		}
		if v.ServerUser != "" {
			m["serverUser"] = v.ServerUser
		}
		if v.SecretKey != "" {
			m["secretKey"] = v.SecretKey
		}
		m["bindAddr"] = defaultString(v.BindAddr, "127.0.0.1")
		// bindPort 允许为负数（只接收其它 visitor 转发的连接），因此照实下发
		m["bindPort"] = v.BindPort

		if v.UseEncryption || v.UseCompression {
			tr := map[string]any{}
			if v.UseEncryption {
				tr["useEncryption"] = true
			}
			if v.UseCompression {
				tr["useCompression"] = true
			}
			m["transport"] = tr
		}

		if typ == "xtcp" {
			if v.KeepTunnelOpen {
				m["keepTunnelOpen"] = true
			}
			if v.MaxRetriesAnHour > 0 {
				m["maxRetriesAnHour"] = v.MaxRetriesAnHour
			}
			if v.MinRetryInterval > 0 {
				m["minRetryInterval"] = v.MinRetryInterval
			}
			if v.FallbackTo != "" {
				m["fallbackTo"] = v.FallbackTo
			}
			if v.FallbackTimeoutMs > 0 {
				m["fallbackTimeoutMs"] = v.FallbackTimeoutMs
			}
			if v.DisableAssistedAddrs {
				m["natTraversal"] = map[string]any{"disableAssistedAddrs": true}
			}
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// buildHealthCheck 健康检查配置：type 为空即视为未启用
func buildHealthCheck(p model.Proxy) map[string]any {
	if strings.TrimSpace(p.HealthCheckType) == "" {
		return nil
	}
	hc := map[string]any{"type": strings.ToLower(strings.TrimSpace(p.HealthCheckType))}
	if p.HealthCheckTimeoutSeconds > 0 {
		hc["timeoutSeconds"] = p.HealthCheckTimeoutSeconds
	}
	if p.HealthCheckMaxFailed > 0 {
		hc["maxFailed"] = p.HealthCheckMaxFailed
	}
	if p.HealthCheckIntervalSeconds > 0 {
		hc["intervalSeconds"] = p.HealthCheckIntervalSeconds
	}
	if p.HealthCheckPath != "" {
		hc["path"] = p.HealthCheckPath
	}
	return hc
}

// buildPlugin 本地服务插件：type + 其余参数。
//
// PluginConfig 是 **JSON 对象**（不是 key=value 行），因为不同插件的参数差异很大，
// 而且参数值本身可能含逗号 —— 用 key=value 解析器会把 JSON 切碎导致参数全丢。
func buildPlugin(p model.Proxy) map[string]any {
	typ := strings.TrimSpace(p.PluginType)
	if typ == "" {
		return nil
	}
	out := map[string]any{"type": typ}
	params, err := ParseJSONObject(p.PluginConfig)
	if err != nil {
		// 校验阶段已拦下非法 JSON，这里兜底不让坏配置流出去
		return out
	}
	for k, v := range params {
		if k == "type" {
			continue // 以 PluginType 为准，避免两处不一致
		}
		out[k] = v
	}
	return out
}

// ParseJSONObject 解析 JSON 对象文本；留空返回 nil
func ParseJSONObject(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, errors.New("插件参数必须是合法的 JSON 对象：" + err.Error())
	}
	return out, nil
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

// parseKV 解析「每行 key=value」形式的元信息（兼容逗号分隔）；值统一按字符串处理
func parseKV(s string) map[string]string {
	raw := parseAnyKV(s)
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		out[k] = strings.TrimSpace(toString(v))
	}
	return out
}

// parseAnyKV 解析 key=value 行，值尽量保留原始 JSON 类型（字符串/数字/布尔）
func parseAnyKV(s string) map[string]any {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	out := map[string]any{}
	for _, line := range strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' }) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, v, found := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !found || k == "" {
			continue
		}
		v = strings.TrimSpace(v)
		// 去掉常见的包裹引号
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		out[k] = coerceScalar(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// coerceScalar 把 true/false/数字还原成 JSON 标量，其余保持字符串
func coerceScalar(v string) any {
	switch strings.ToLower(v) {
	case "true":
		return true
	case "false":
		return false
	}
	var num json.Number
	if err := json.Unmarshal([]byte(v), &num); err == nil {
		return num
	}
	return v
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		b, _ := json.Marshal(v)
		return strings.Trim(string(b), `"`)
	}
}

func defaultString(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
