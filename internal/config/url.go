package config

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

var hostnamePattern = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9.-]*[a-zA-Z0-9])?$`)

// PublicAddress 只接受根路径的 HTTP(S) 地址，拒绝凭据、参数及命令解释器特殊字符。
func PublicAddress(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("请显式配置 PublicURL（-public-url 或 DFPANEL_PUBLIC_URL）")
	}
	if strings.ContainsAny(raw, "\r\n\t '\"`$;|&<>\\%!") {
		return "", fmt.Errorf("PublicURL 包含非法字符")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("PublicURL 必须是根路径 HTTP(S) 地址")
	}
	host := u.Hostname()
	if net.ParseIP(host) == nil && (!hostnamePattern.MatchString(host) || len(host) > 253) {
		return "", fmt.Errorf("PublicURL 主机名不合法")
	}
	if port := u.Port(); port != "" {
		if _, err := net.LookupPort("tcp", port); err != nil {
			return "", fmt.Errorf("PublicURL 端口不合法")
		}
	}
	return strings.TrimRight(raw, "/"), nil
}
