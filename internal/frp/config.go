package frp

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"dfpanel/internal/model"
)

// AuthServerConfig frps 鉴权配置（auth）
type AuthServerConfig struct {
	Method           string                `json:"method,omitempty"` // token / oidc，frp 默认 token
	AdditionalScopes []string              `json:"additionalScopes,omitempty"`
	Token            string                `json:"token,omitempty"`
	// TokenSource 与 Token 互斥：把 token 的来源交给文件或命令
	TokenSource *ValueSource          `json:"tokenSource,omitempty"`
	OIDC        *AuthOIDCServerConfig `json:"oidc,omitempty"`
}

// AuthOIDCServerConfig auth.method = oidc 时生效
type AuthOIDCServerConfig struct {
	Issuer          string `json:"issuer,omitempty"`
	Audience        string `json:"audience,omitempty"`
	SkipExpiryCheck bool   `json:"skipExpiryCheck,omitempty"`
	SkipIssuerCheck bool   `json:"skipIssuerCheck,omitempty"`
}

// TLSServerConfig transport.tls，force 与证书配置同级
type TLSServerConfig struct {
	Force bool `json:"force,omitempty"`
	TLSConfig
}

// ServerTransportConfig 服务端网络层配置（transport）
type ServerTransportConfig struct {
	TCPMux                  *bool            `json:"tcpMux,omitempty"`
	TCPMuxKeepaliveInterval int              `json:"tcpMuxKeepaliveInterval,omitempty"`
	TCPKeepAlive            int              `json:"tcpKeepalive,omitempty"`
	MaxPoolCount            int              `json:"maxPoolCount,omitempty"`
	HeartbeatTimeout        int              `json:"heartbeatTimeout,omitempty"`
	QUIC                    *QUICOptions     `json:"quic,omitempty"`
	TLS                     *TLSServerConfig `json:"tls,omitempty"`
}

// SSHTunnelGateway SSH 隧道网关（sshTunnelGateway，frp v0.68+）
type SSHTunnelGateway struct {
	BindPort              int    `json:"bindPort,omitempty"`
	PrivateKeyFile        string `json:"privateKeyFile,omitempty"`
	AutoGenPrivateKeyPath string `json:"autoGenPrivateKeyPath,omitempty"`
	AuthorizedKeysFile    string `json:"authorizedKeysFile,omitempty"`
}

// FrpsConfig 与 frps.json 一一对应
// 参考：https://gofrp.org/zh-cn/docs/reference/server-configures/
type FrpsConfig struct {
	Auth AuthServerConfig `json:"auth,omitempty"`

	BindAddr      string `json:"bindAddr,omitempty"`
	BindPort      int    `json:"bindPort"`
	KCPBindPort   int    `json:"kcpBindPort,omitempty"`
	QUICBindPort  int    `json:"quicBindPort,omitempty"`
	ProxyBindAddr string `json:"proxyBindAddr,omitempty"`

	VhostHTTPPort         int    `json:"vhostHTTPPort,omitempty"`
	VhostHTTPTimeout      int    `json:"vhostHTTPTimeout,omitempty"`
	VhostHTTPSPort        int    `json:"vhostHTTPSPort,omitempty"`
	TCPMuxHTTPConnectPort int    `json:"tcpmuxHTTPConnectPort,omitempty"`
	TCPMuxPassthrough     bool   `json:"tcpmuxPassthrough,omitempty"`
	SubDomainHost         string `json:"subDomainHost,omitempty"`
	Custom404Page         string `json:"custom404Page,omitempty"`

	SSHTunnelGateway *SSHTunnelGateway `json:"sshTunnelGateway,omitempty"`

	WebServer        *WebServerConfig `json:"webServer,omitempty"`
	EnablePrometheus bool             `json:"enablePrometheus,omitempty"`

	Log       *LogConfig             `json:"log,omitempty"`
	Transport *ServerTransportConfig `json:"transport,omitempty"`

	DetailedErrorsToClient          *bool `json:"detailedErrorsToClient,omitempty"`
	MaxPortsPerClient               int   `json:"maxPortsPerClient,omitempty"`
	UserConnTimeout                 int   `json:"userConnTimeout,omitempty"`
	UDPPacketSize                   int   `json:"udpPacketSize,omitempty"`
	NatHoleAnalysisDataReserveHours int   `json:"natholeAnalysisDataReserveHours,omitempty"`

	AllowPorts []PortRange `json:"allowPorts,omitempty"`

	// HTTPPlugins 服务端插件，按 ops 在登录 / 新建代理等时机回调外部 HTTP 服务
	HTTPPlugins []HTTPPluginOptions `json:"httpPlugins,omitempty"`
}

// ParseHTTPPlugins 解析服务端插件配置（JSON 数组文本）。
// 留空表示不使用插件；格式非法时返回错误，避免把坏配置下发到 frps。
func ParseHTTPPlugins(raw string) ([]HTTPPluginOptions, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var out []HTTPPluginOptions
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, errors.New("服务端插件配置不是合法的 JSON 数组：" + err.Error())
	}
	for i := range out {
		if strings.TrimSpace(out[i].Name) == "" {
			return nil, errors.New("服务端插件缺少 name")
		}
		if strings.TrimSpace(out[i].Addr) == "" {
			return nil, errors.New("服务端插件缺少 addr")
		}
		if strings.TrimSpace(out[i].Path) == "" {
			out[i].Path = "/handler"
		}
	}
	return out, nil
}

// BuildFrpsJSON 根据面板配置生成 frps.json 文本
func BuildFrpsJSON(s *model.FrpsServer, logPath string) (string, error) {
	cfg := FrpsConfig{
		Auth: AuthServerConfig{
			Method:           defaultStr(s.AuthMethod, "token"),
			AdditionalScopes: ParseScopes(s.AuthAdditionalScopes),
		},
		BindAddr:      defaultStr(s.BindAddr, "0.0.0.0"),
		BindPort:      defaultInt(s.BindPort, 7000),
		KCPBindPort:   s.KCPBindPort,
		QUICBindPort:  s.QUICBindPort,
		ProxyBindAddr: s.ProxyBindAddr,

		VhostHTTPPort:         s.VhostHTTPPort,
		VhostHTTPTimeout:      s.VhostHTTPTimeout,
		VhostHTTPSPort:        s.VhostHTTPSPort,
		TCPMuxHTTPConnectPort: s.TCPMuxHTTPConnectPort,
		TCPMuxPassthrough:     s.TCPMuxPassthrough,
		SubDomainHost:         s.SubdomainHost,
		Custom404Page:         s.Custom404Page,

		MaxPortsPerClient:               s.MaxPortsPerClient,
		UserConnTimeout:                 s.UserConnTimeout,
		UDPPacketSize:                   s.UDPPacketSize,
		NatHoleAnalysisDataReserveHours: s.NatHoleAnalysisDataReserveHours,
		EnablePrometheus:                s.EnablePrometheus,
		AllowPorts:                      ParseAllowPorts(s.AllowPorts),
	}

	// tokenSource 与 token 互斥：配了来源就不再写 token
	if path := strings.TrimSpace(s.AuthTokenSourcePath); path != "" {
		cfg.Auth.TokenSource = &ValueSource{
			Type: defaultStr(s.AuthTokenSourceType, "file"),
			File: &FileSource{Path: path},
		}
	} else {
		cfg.Auth.Token = s.AuthToken
	}

	// 服务端插件（httpPlugins）
	if plugins, err := ParseHTTPPlugins(s.AuthHTTPPlugins); err != nil {
		return "", err
	} else if len(plugins) > 0 {
		cfg.HTTPPlugins = plugins
	}

	// 非零值才写入，未配置时交给 frps 自身的默认值
	if s.DetailedErrorsToClient != nil {
		cfg.DetailedErrorsToClient = s.DetailedErrorsToClient
	}

	if s.AuthOIDCIssuer != "" {
		cfg.Auth.OIDC = &AuthOIDCServerConfig{
			Issuer:          s.AuthOIDCIssuer,
			Audience:        s.AuthOIDCAudience,
			SkipExpiryCheck: s.AuthOIDCSkipExpiryCheck,
			SkipIssuerCheck: s.AuthOIDCSkipIssuerCheck,
		}
	}

	// frp 默认 tcpMux = true，关闭时必须显式写出 false
	tcpMux := s.TcpMux == nil || *s.TcpMux
	transport := &ServerTransportConfig{
		TCPMux:                  &tcpMux,
		TCPMuxKeepaliveInterval: s.TcpMuxKeepaliveInterval,
		TCPKeepAlive:            s.TcpKeepalive,
		MaxPoolCount:            s.MaxPoolCount,
		HeartbeatTimeout:        s.HeartbeatTimeout,
	}
	if s.QuicKeepalivePeriod > 0 || s.QuicMaxIdleTimeout > 0 || s.QuicMaxIncomingStreams > 0 {
		transport.QUIC = &QUICOptions{
			KeepalivePeriod:    s.QuicKeepalivePeriod,
			MaxIdleTimeout:     s.QuicMaxIdleTimeout,
			MaxIncomingStreams: s.QuicMaxIncomingStreams,
		}
	}
	if s.TLSForce || s.TLSCertFile != "" || s.TLSKeyFile != "" || s.TLSTrustedCaFile != "" || s.TLSServerName != "" {
		transport.TLS = &TLSServerConfig{
			Force: s.TLSForce,
			TLSConfig: TLSConfig{
				CertFile:      s.TLSCertFile,
				KeyFile:       s.TLSKeyFile,
				TrustedCaFile: s.TLSTrustedCaFile,
				ServerName:    s.TLSServerName,
			},
		}
	}
	cfg.Transport = transport

	// Dashboard 默认不启用；未启用（或端口为 0）时完全不输出 webServer 段
	dashboardOn := s.DashboardEnabled != nil && *s.DashboardEnabled
	if dashboardOn && s.DashboardPort > 0 {
		ws := &WebServerConfig{
			Addr:        defaultStr(s.DashboardAddr, "0.0.0.0"),
			Port:        s.DashboardPort,
			User:        s.DashboardUser,
			Password:    s.DashboardPwd,
			AssetsDir:   s.DashboardAssetsDir,
			PprofEnable: s.DashboardPprofEnable,
		}
		if s.DashboardTLSCertFile != "" || s.DashboardTLSKeyFile != "" {
			ws.TLS = &TLSConfig{CertFile: s.DashboardTLSCertFile, KeyFile: s.DashboardTLSKeyFile}
		}
		cfg.WebServer = ws
	}

	if s.SSHGatewayBindPort > 0 {
		cfg.SSHTunnelGateway = &SSHTunnelGateway{
			BindPort:              s.SSHGatewayBindPort,
			PrivateKeyFile:        s.SSHGatewayPrivateKeyFile,
			AutoGenPrivateKeyPath: s.SSHGatewayAutoGenPrivateKeyPath,
			AuthorizedKeysFile:    s.SSHGatewayAuthorizedKeysFile,
		}
	}

	cfg.Log = &LogConfig{
		To:                logPath,
		Level:             defaultStr(s.LogLevel, "info"),
		MaxDays:           s.LogMaxDays,
		DisablePrintColor: s.LogDisablePrintColor,
	}

	return renderWithExtra(cfg, s.ExtraJSON)
}

// renderWithExtra 序列化配置并与「额外 JSON 片段」做顶层浅合并后缩进输出
func renderWithExtra(cfg any, extra string) (string, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}

	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}

	if over := strings.TrimSpace(extra); over != "" {
		add := map[string]any{}
		if err := json.Unmarshal([]byte(over), &add); err != nil {
			return "", errors.New("额外配置片段不是合法的 JSON 对象：" + err.Error())
		}
		for k, v := range add {
			out[k] = v
		}
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ParseAllowPorts 解析 "2000-3000,4000,5000-5010" 为端口范围列表
// 单一端口用 single 表示，连续区间用 start + end 表示
func ParseAllowPorts(s string) []PortRange {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var res []PortRange
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if i := strings.Index(part, "-"); i > 0 {
			a, err1 := strconv.Atoi(strings.TrimSpace(part[:i]))
			b, err2 := strconv.Atoi(strings.TrimSpace(part[i+1:]))
			if err1 != nil || err2 != nil {
				continue
			}
			if b < a {
				a, b = b, a
			}
			res = append(res, PortRange{Start: a, End: b})
			continue
		}
		if v, err := strconv.Atoi(part); err == nil {
			res = append(res, PortRange{Single: v})
		}
	}
	return res
}

// ParseScopes 解析逗号分隔的鉴权范围（HeartBeats / NewWorkConns）
func ParseScopes(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var res []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			res = append(res, p)
		}
	}
	return res
}

func defaultStr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func defaultInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}
